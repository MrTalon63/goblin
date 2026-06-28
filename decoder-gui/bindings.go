package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	goRuntime "runtime"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ---------- Wails bindings ----------

// HistoryEntry is a serializable history entry
type HistoryEntry struct {
	ImgID       uint16 `json:"imgId"`
	PacketCount int    `json:"packetCount"`
	APID        int    `json:"apid"`
	CompletedAt string `json:"completedAt"`
	Image       string `json:"image"`
	SavedPath   string `json:"savedPath"`
}

// ImageInfo holds metadata for parsed images
type ImageInfo struct {
	Data        string `json:"data"`
	ImgID       uint16 `json:"imgId"`
	PacketCount int    `json:"packetCount"`
	APID        int    `json:"apid"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Quality     int    `json:"quality"`
}

// TelemetrySnapshot returns current telemetry data as JSON
func (a *App) TelemetrySnapshot() string {

	// Snapshot totalImages, corruptCount, and APID configs under cfgMu
	a.cfgMu.Lock()
	total := a.totalImages
	errors := a.corruptCount
	apidCfgs := make(map[int]APIDConfig, len(a.cfg.APIDs))
	for k, v := range a.cfg.APIDs {
		apidCfgs[k] = v
	}
	a.cfgMu.Unlock()

	// Snapshot firstPacketTime and counters under apidMu
	a.apidMu.Lock()
	firstTime := a.firstPacketTime
	var apidList []map[string]interface{}
	for k, v := range a.apidCounters {
		apidCfg := apidCfgs[k]
		apidList = append(apidList, map[string]interface{}{
			"apid":    k,
			"type":    apidCfg.Type,
			"port":    apidCfg.Port,
			"packets": v,
		})
	}
	a.apidMu.Unlock()

	uptimeStr := "0s"
	if !firstTime.IsZero() {
		uptimeStr = time.Since(firstTime).Round(time.Second).String()
	}

	// Snapshot recording state under recMutex
	a.recMutex.Lock()
	recording := a.recording
	a.recMutex.Unlock()

	data := map[string]interface{}{
		"apidList":  apidList,
		"total":     total,
		"errors":    errors,
		"uptime":    uptimeStr,
		"recording": recording,
	}
	a.telemMutex.Lock()
	core := a.telemCore
	timesync := a.telemTimesync
	dynamic := a.telemDynamic
	a.telemMutex.Unlock()

	if core != nil {
		data["core"] = map[string]interface{}{
			"callsign":     core.Callsign,
			"latitude":     core.Latitude,
			"longitude":    core.Longitude,
			"altitude":     core.Altitude,
			"battVoltage":  core.BattVoltage,
			"tempInternal": core.TempInternal,
			"tempExternal": core.TempExternal,
			"time":         core.ComputedTime.Format(time.RFC3339Nano),
		}
	}
	if timesync != nil {
		lines := timesync.DisplayLines()
		if len(lines) > 0 {
			data["timesync"] = lines[0]
		}
	}
	if dynamic != nil {
		data["payloadName"] = dynamic.Name
	}

	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal telemetry snapshot: %v", err)
		return "{}"
	}
	return string(b)
}

// LatestImage returns current decoded image as base64
func (a *App) LatestImage() string {
	if a.lastImage == nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(a.lastImage)
}

// GetHistory returns all history entries with images as base64
func (a *App) GetHistory() string {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	entries := make([]HistoryEntry, len(a.history))
	for i, h := range a.history {
		entries[i] = HistoryEntry{
			ImgID:       h.ImgID,
			PacketCount: h.PacketCount,
			APID:        h.APID,
			CompletedAt: h.CompletedAt.Format(time.RFC3339),
			Image:       base64.StdEncoding.EncodeToString(h.JPEG),
			SavedPath:   h.SavedPath,
		}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		log.Printf("Failed to marshal history: %v", err)
		return "[]"
	}
	return string(b)
}

// GetSettings returns current config as JSON
func (a *App) GetSettings() string {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	b, err := json.Marshal(a.cfg)
	if err != nil {
		log.Printf("Failed to marshal settings: %v", err)
		return "{}"
	}
	return string(b)
}

// SaveSettings saves new config and restarts receivers
func (a *App) SaveSettings(jsonConfig string) string {
	var newCfg Config
	if err := json.Unmarshal([]byte(jsonConfig), &newCfg); err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	if err := SaveConfig(newCfg); err != nil {
		log.Printf("Failed to save config: %v", err)
		return fmt.Sprintf("error: %v", err)
	}
	a.cfgMu.Lock()
	a.cfg = newCfg
	a.session = NewSessionDir(newCfg.SaveDir)
	if a.tileCache != nil {
		a.tileCache.UpdateBaseDir(newCfg.TileDir)
	}

	a.telemMutex.Lock()
	a.sessionCallsign = ""
	a.callsignCounts = make(map[string]int)
	a.telemMutex.Unlock()

	// Restart receivers under lock
	for apid, r := range a.udpReceivers {
		r.Close()
		delete(a.udpReceivers, apid)
	}
	a.startAllReceiversLocked()
	a.cfgMu.Unlock()

	a.restartCache(newCfg.CacheFile)

	if a.centralUploader != nil {
		a.centralUploader.Start(newCfg)
	}

	return "ok"
}

// StartRecording starts global recording
func (a *App) StartRecording() string {
	a.recMutex.Lock()
	defer a.recMutex.Unlock()
	if a.recording {
		return "already recording"
	}
	fname := "recording_" + time.Now().Format("20060102_150405") + ".bin"
	f, err := os.Create(fname)
	if err != nil {
		return fmt.Sprintf("error: %v", err)
	}
	a.recFile = f
	a.recording = true
	return "ok"
}

// StopRecording stops global recording
func (a *App) StopRecording() string {
	a.recMutex.Lock()
	defer a.recMutex.Unlock()
	if !a.recording || a.recFile == nil {
		return "not recording"
	}
	a.recFile.Close()
	a.recFile = nil
	a.recording = false
	return "ok"
}

// ClearCache clears the packet cache
func (a *App) ClearCache() string {
	a.cfgMu.Lock()
	cachePath := a.cachePath
	a.cfgMu.Unlock()
	if cachePath == "" {
		return "no cache configured"
	}
	a.restartCache(cachePath)
	for _, p := range []string{cachePath} {
		for i := 0; i <= 256; i++ {
			name := p
			if i > 0 {
				name = fmt.Sprintf("%s_%03d.ssdv", p, i)
			}
			os.Remove(name)
		}
	}
	a.cfgMu.Lock()
	a.decoder = nil
	a.hasInit = false
	a.packetCount = 0
	a.corruptCount = 0
	a.seenPackets = make(map[uint64]bool)
	a.lastImage = nil
	a.lastImgID = 0
	a.cfgMu.Unlock()
	return "ok"
}

// GetSessionPath returns current session directory
func (a *App) GetSessionPath() string {
	return a.session.SessionPath()
}

// GetPayloadName returns current payload name
func (a *App) GetPayloadName() string {
	a.telemMutex.Lock()
	defer a.telemMutex.Unlock()
	if a.telemDynamic != nil {
		return a.telemDynamic.Name
	}
	return ""
}

// GetTileServerURL returns the local tile server base URL
func (a *App) GetTileServerURL() string {
	if a.tileCache == nil {
		return ""
	}
	return a.tileCache.URL()
}

// PreloadTiles starts downloading tiles for the given area
func (a *App) PreloadTiles(configJSON string) string {
	if a.tileCache == nil {
		return "no tile cache"
	}
	return a.tileCache.PreloadTiles(configJSON)
}

// CancelPreload stops any ongoing tile preload.
func (a *App) CancelPreload() string {
	if a.tileCache == nil {
		return "no tile cache"
	}
	a.tileCache.CancelPreload()
	return "cancelled"
}

// UpdateRateLimit dynamically adjusts tile download concurrency.
func (a *App) UpdateRateLimit(concurrency int) string {
	if a.tileCache == nil {
		return "no tile cache"
	}
	a.tileCache.UpdateRateLimit(concurrency)
	return "ok"
}

// EstimateTiles returns an estimate of tile count, time, and size without downloading
func (a *App) EstimateTiles(configJSON string) string {
	if a.tileCache == nil {
		return "no tile cache"
	}
	return a.tileCache.EstimateTiles(configJSON)
}

func (a *App) broadcastTelemetry() {
	// Push to frontend via events
	payload := a.TelemetrySnapshot()
	runtime.EventsEmit(a.ctx, "telemetry", payload)
}

// GetVersion returns the application version
func (a *App) GetVersion() string {
	return Version
}

// GetLatestImageInfo returns metadata about the latest image as JSON
func (a *App) GetLatestImageInfo() string {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	if a.lastImage == nil {
		return "{}"
	}
	w, h, q := 0, 0, 0
	if a.decoder != nil {
		w = int(a.decoder.Width())
		h = int(a.decoder.Height())
		q = int(a.decoder.Quality())
	}
	info := ImageInfo{
		Data:        base64.StdEncoding.EncodeToString(a.lastImage),
		ImgID:       a.lastImgID,
		PacketCount: a.packetCount,
		APID:        a.ssdvAPID,
		Width:       w,
		Height:      h,
		Quality:     q,
	}
	b, err := json.Marshal(info)
	if err != nil {
		log.Printf("Failed to marshal image info: %v", err)
		return "{}"
	}
	return string(b)
}

// SelectDirectory opens a native directory chooser dialog and returns the selected path
func (a *App) SelectDirectory(title string) string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
	if err != nil {
		log.Printf("Error opening directory dialog: %v", err)
		return ""
	}
	return dir
}

// OpenSessionFolder opens the current session folder in the system file explorer
func (a *App) OpenSessionFolder() {
	dir := a.session.SessionPath()
	var cmd *exec.Cmd
	switch goRuntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default: // linux, freebsd, etc
		cmd = exec.Command("xdg-open", dir)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open folder: %v", err)
	}
}

// IsSessionActive returns whether the active receive session directory has been created.
func (a *App) IsSessionActive() bool {
	return a.session.IsCreated()
}


