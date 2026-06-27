package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"
)

type ImageHistoryEntry struct {
	JPEG        []byte
	ImgID       uint16
	CompletedAt time.Time
	PacketCount int
	SavedPath   string
	APID        int
}

// App struct
type App struct {
	ctx context.Context

	cfgMu       sync.Mutex
	cfg         Config
	cache       *PacketCache
	decoder     *Decoder
	cachePath   string
	packetCount int
	lastImage   []byte
	lastImgID   uint16
	hasInit     bool
	imageSaved  bool

	seenPackets  map[uint64]bool
	corruptCount int

	history     []ImageHistoryEntry
	historyIdx  int
	totalImages int
	ssdvAPID    int

	recording bool
	recFile   *os.File
	recMutex  sync.Mutex

	session        *SessionDir
	recordingFiles map[int]*os.File
	udpReceivers   map[int]*UDPReceiver
	apidMu         sync.Mutex
	apidCounters   map[int]int64

	firstPacketTime time.Time

	telemMutex    sync.Mutex
	telemTimesync *TimeSyncData
	telemCore     *CoreTelemetryData
	telemDynamic  *DynamicTelemetryData

	// image decoder state
	snapshotMu   sync.Mutex
	snapshotData []byte
	snapshotID   uint16
	snapshotPkts int
	snapshotEOI  bool
	hasSnapshot  bool

	lastPktTime time.Time

	knownResW      int
	knownResH      int
	knownResImgID  uint16
	lastHistoryAdd map[uint16]time.Time

	autoRecFile   *os.File
	autoRecPath   string
	autoRecFiles  []string
	autoRecording bool

	tileCache *TileCache
}

// NewApp creates a new App application struct
func NewApp() *App {
	cfg := LoadConfig()
	return &App{
		cfg:            cfg,
		seenPackets:    make(map[uint64]bool),
		historyIdx:     -1,
		recordingFiles: make(map[int]*os.File),
		udpReceivers:   make(map[int]*UDPReceiver),
		apidCounters:   make(map[int]int64),
		session:        NewSessionDir(cfg.SaveDir),
		tileCache:      NewTileCache(cfg.TileDir, cfg.TilePort, cfg.TileConcurrency),
	}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.lastPktTime = time.Now()
	a.restartCache(a.cfg.CacheFile)
	a.startAllReceivers()
	a.lastHistoryAdd = make(map[uint16]time.Time)

	// Start tile cache server with Wails context for event emitting
	a.tileCache.SetWailsContext(ctx)
	if err := a.tileCache.Start(); err != nil {
		log.Printf("Tile cache failed to start: %v", err)
	}

	// Ticker for snapshot display and stale decoder checks
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.snapshotMu.Lock()
				if a.hasSnapshot {
					data := a.snapshotData
					id := a.snapshotID
					pkts := a.snapshotPkts
					eoi := a.snapshotEOI
					a.hasSnapshot = false
					a.snapshotData = nil
					a.snapshotMu.Unlock()
					a.lastImgID = id
					a.updateImage(data, id, eoi, pkts)
					if !eoi {
						a.maybeAddSnapshotToHistory(data, id, pkts)
					}
				} else {
					a.snapshotMu.Unlock()
				}
				a.cfgMu.Lock()
				if a.hasInit && a.decoder != nil && time.Since(a.lastPktTime) > 120*time.Second {
					log.Printf("Decoder stale, resetting")
					a.resetDecoderLocked()
				}
				a.cfgMu.Unlock()
			}
		}
	}()
}

func (a *App) restartCache(path string) {
	if a.cache != nil {
		if err := a.cache.Close(); err != nil {
			log.Printf("Failed to close packet cache: %v", err)
		}
		a.cache = nil
	}
	if path != "" {
		c, err := NewPacketCache(path)
		if err != nil {
			log.Printf("Failed to open packet cache %s: %v", path, err)
		} else {
			a.cache = c
			a.cachePath = path
		}
	}
}

// OnShutdown cleanup
func (a *App) shutdown(ctx context.Context) {
	a.stopAutoRecording()
	a.recMutex.Lock()
	for apid, f := range a.recordingFiles {
		f.Close()
		delete(a.recordingFiles, apid)
	}
	a.recMutex.Unlock()

	a.cfgMu.Lock()
	for apid, r := range a.udpReceivers {
		r.Close()
		delete(a.udpReceivers, apid)
	}
	a.cfgMu.Unlock()

	if a.cache != nil {
		a.cache.Close()
	}
	if a.tileCache != nil {
		a.tileCache.Stop()
	}
}

func imageIDFromCCSDSPayload(pkt []byte) uint16 {
	if len(pkt) < 2 {
		return 0
	}
	return uint16(pkt[0])<<8 | uint16(pkt[1])
}

func packetIDFromCCSDSPayload(pkt []byte) uint32 {
	if len(pkt) < 5 {
		return 0
	}
	return uint32(pkt[2])<<16 | uint32(pkt[3])<<8 | uint32(pkt[4])
}

func eoiFromCCSDSPayload(pkt []byte) bool {
	if len(pkt) < 9 {
		return false
	}
	return (pkt[8]>>2)&1 == 1
}
