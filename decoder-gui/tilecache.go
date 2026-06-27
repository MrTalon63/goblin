package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TileCache manages an HTTP tile server with local caching.
type TileCache struct {
	baseDir     string
	port        int
	server      *http.Server
	client      *http.Client
	semMu       sync.Mutex // guards sem field
	sem         chan struct{}
	concurrency int
	lastReq     time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	wailsCtx    context.Context // set by app startup for EventsEmit
}

// NewTileCache creates a new tile cache manager.
func NewTileCache(baseDir string, port, concurrency int) *TileCache {
	if concurrency < 1 {
		concurrency = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &TileCache{
		baseDir:     baseDir,
		port:        port,
		concurrency: concurrency,
		client:      &http.Client{Timeout: 15 * time.Second},
		sem:         make(chan struct{}, concurrency),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start launches the HTTP tile server on the configured port (or a random one if 0).
func (tc *TileCache) Start() error {
	if tc.port == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return err
		}
		tc.port = listener.Addr().(*net.TCPAddr).Port
		tc.server = &http.Server{Handler: tc}
		go tc.server.Serve(listener)
	} else {
		tc.server = &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", tc.port), Handler: tc}
		go tc.server.ListenAndServe()
	}
	fmt.Printf("Tile server started on http://127.0.0.1:%d/tiles/{z}/{x}/{y}.png\n", tc.port)
	return nil
}

// Stop shuts down the tile server gracefully.
func (tc *TileCache) Stop() error {
	if tc.cancel != nil {
		tc.cancel()
	}
	if tc.server != nil {
		return tc.server.Close()
	}
	return nil
}

// acquireSem acquires the semaphore slot under lock to avoid send-on-closed panic.
func (tc *TileCache) acquireSem() chan struct{} {
	tc.semMu.Lock()
	sem := tc.sem
	tc.semMu.Unlock()
	sem <- struct{}{}
	return sem
}

// releaseSem releases the semaphore slot.
func (tc *TileCache) releaseSem(sem chan struct{}) {
	<-sem
}

// ServeHTTP implements http.Handler.
func (tc *TileCache) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/tiles/")
	parts := splitPath(path)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	z, err1 := strconv.Atoi(parts[0])
	x, err2 := strconv.Atoi(parts[1])
	yStr := parts[2]
	if err1 != nil || err2 != nil || !strings.HasSuffix(yStr, ".png") {
		http.NotFound(w, r)
		return
	}
	y, err := strconv.Atoi(yStr[:len(yStr)-4])
	if err != nil {
		http.NotFound(w, r)
		return
	}

	tilePath := filepath.Join(tc.baseDir, fmt.Sprintf("%d", z), fmt.Sprintf("%d", x), fmt.Sprintf("%d.png", y))
	if data, err := os.ReadFile(tilePath); err == nil {
		w.Header().Set("Content-Type", "image/png")
		w.Write(data)
		return
	}

	sem := tc.acquireSem()
	data, err := tc.downloadTile(z, x, y)
	tc.releaseSem(sem)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Write(data)
}

func (tc *TileCache) downloadTile(z, x, y int) ([]byte, error) {
	url := fmt.Sprintf("https://tile.openstreetmap.org/%d/%d/%d.png", z, x, y)
	req, err := http.NewRequestWithContext(tc.ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "GoblinDecoder/1.0")
	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	tilePath := filepath.Join(tc.baseDir, fmt.Sprintf("%d", z), fmt.Sprintf("%d", x), fmt.Sprintf("%d.png", y))
	if err := os.MkdirAll(filepath.Dir(tilePath), 0755); err == nil {
		os.WriteFile(tilePath, data, 0644)
	}
	return data, nil
}

// PreloadTiles downloads tiles for the given bounding box and zoom range.
func (tc *TileCache) PreloadTiles(configJSON string) string {
	tc.cancelPreload()
	tc.ctx, tc.cancel = context.WithCancel(context.Background())
	go tc.preload(configJSON)
	return "started"
}

// CancelPreload stops any ongoing preload.
func (tc *TileCache) CancelPreload() {
	tc.cancelPreload()
}

func (tc *TileCache) cancelPreload() {
	if tc.cancel != nil {
		tc.cancel()
	}
}

// EstimateTiles returns the tile count estimate without downloading.
func (tc *TileCache) EstimateTiles(configJSON string) string {
	var cfg struct {
		LatMin  float64 `json:"latMin"`
		LatMax  float64 `json:"latMax"`
		LngMin  float64 `json:"lngMin"`
		LngMax  float64 `json:"lngMax"`
		ZoomMin int     `json:"zoomMin"`
		ZoomMax int     `json:"zoomMax"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "error"
	}
	total := 0
	for z := cfg.ZoomMin; z <= cfg.ZoomMax; z++ {
		xMin, xMax, yMin, yMax := boundsToTileRange(cfg.LatMin, cfg.LatMax, cfg.LngMin, cfg.LngMax, z)
		total += (xMax - xMin + 1) * (yMax - yMin + 1)
	}
	concurrency := tc.concurrency
	if concurrency < 1 {
		concurrency = 1
	}
	// ~15KB per tile, estimate ~1s per tile with chosen concurrency
	secs := (total + concurrency - 1) / concurrency
	sizeMB := float64(total) * 15.0 / 1024.0
	return fmt.Sprintf("%d tiles, ~%ds, ~%.1fMB", total, secs, sizeMB)
}

func (tc *TileCache) emit(msg string) {
	// Use Wails context if available, otherwise fall back to internal context
	ctx := tc.wailsCtx
	if ctx == nil {
		ctx = tc.ctx
	}
	runtime.EventsEmit(ctx, "tileProgress", msg)
}

func (tc *TileCache) preload(configJSON string) {
	var cfg struct {
		LatMin  float64 `json:"latMin"`
		LatMax  float64 `json:"latMax"`
		LngMin  float64 `json:"lngMin"`
		LngMax  float64 `json:"lngMax"`
		ZoomMin int     `json:"zoomMin"`
		ZoomMax int     `json:"zoomMax"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		tc.emit(fmt.Sprintf("error: %v", err))
		return
	}

	total := 0
	for z := cfg.ZoomMin; z <= cfg.ZoomMax; z++ {
		xMin, xMax, yMin, yMax := boundsToTileRange(cfg.LatMin, cfg.LatMax, cfg.LngMin, cfg.LngMax, z)
		total += (xMax - xMin + 1) * (yMax - yMin + 1)
	}
	loaded := 0

	for z := cfg.ZoomMin; z <= cfg.ZoomMax; z++ {
		xMin, xMax, yMin, yMax := boundsToTileRange(cfg.LatMin, cfg.LatMax, cfg.LngMin, cfg.LngMax, z)
		for x := xMin; x <= xMax; x++ {
			for y := yMin; y <= yMax; y++ {
				select {
				case <-tc.ctx.Done():
					tc.emit(fmt.Sprintf("cancelled %d/%d", loaded, total))
					return
				default:
				}
				// Skip if already cached
				tilePath := filepath.Join(tc.baseDir, fmt.Sprintf("%d", z), fmt.Sprintf("%d", x), fmt.Sprintf("%d.png", y))
				if _, err := os.Stat(tilePath); err == nil {
					loaded++
					if loaded%10 == 0 || loaded == total {
						tc.emit(fmt.Sprintf("%d/%d", loaded, total))
					}
					continue
				}

				sem := tc.acquireSem()
				data, err := tc.downloadTile(z, x, y)
				tc.releaseSem(sem)
				if err == nil {
					_ = data
					loaded++
					if loaded%10 == 0 || loaded == total {
						tc.emit(fmt.Sprintf("%d/%d", loaded, total))
					}
				}
			}
		}
	}
	tc.emit(fmt.Sprintf("done %d/%d", loaded, total))
}

func latLngToTileXY(lat, lng float64, zoom int) (x, y int) {
	n := 1 << zoom
	x = int((lng + 180.0) / 360.0 * float64(n))
	latRad := lat * math.Pi / 180.0
	y = int((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * float64(n))
	return
}

func boundsToTileRange(latMin, latMax, lngMin, lngMax float64, zoom int) (xMin, xMax, yMin, yMax int) {
	x1, y1 := latLngToTileXY(latMax, lngMin, zoom) // NW corner
	x2, y2 := latLngToTileXY(latMin, lngMax, zoom) // SE corner
	n := 1 << zoom
	return max(0, x1), min(n-1, x2), max(0, y1), min(n-1, y2)
}

// URL returns the tile server base URL for the frontend.
func (tc *TileCache) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", tc.port)
}

// SetWailsContext sets the Wails app context for emitting events.
func (tc *TileCache) SetWailsContext(ctx context.Context) {
	tc.wailsCtx = ctx
}

// UpdateRateLimit dynamically adjusts concurrency.
// Safe to call while preload is running.
func (tc *TileCache) UpdateRateLimit(concurrency int) {
	tc.semMu.Lock()
	defer tc.semMu.Unlock()
	if concurrency < 1 {
		concurrency = 1
	}
	tc.concurrency = concurrency
	tc.sem = make(chan struct{}, concurrency)
}

func splitPath(p string) []string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	var parts []string
	var cur string
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			if cur != "" {
				parts = append(parts, cur)
				cur = ""
			}
		} else {
			cur += string(p[i])
		}
	}
	if cur != "" {
		parts = append(parts, cur)
	}
	return parts
}
