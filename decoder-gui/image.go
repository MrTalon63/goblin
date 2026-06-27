package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateImage stores the latest decoded JPEG in memory.
func (a *App) updateImage(jpg []byte, imgID uint16, eoi bool, pktCount int) {
	if jpg == nil {
		return
	}
	a.lastImage = jpg
	a.lastImgID = imgID
	a.packetCount = pktCount
}

func (a *App) startAutoRecording(imgID uint16) {
	f, err := os.CreateTemp("", fmt.Sprintf("auto_recv_%d_*.bin", imgID))
	if err != nil {
		log.Printf("Failed to create auto-recording file: %v", err)
		return
	}
	a.recMutex.Lock()
	a.autoRecFile = f
	a.autoRecording = true
	a.autoRecPath = f.Name()
	a.autoRecFiles = append(a.autoRecFiles, a.autoRecPath)
	a.recMutex.Unlock()
}

func (a *App) stopAutoRecording() {
	a.recMutex.Lock()
	if a.autoRecording && a.autoRecFile != nil {
		if err := a.autoRecFile.Close(); err != nil {
			log.Printf("Failed to close auto-recording: %v", err)
		}
	}
	a.autoRecording = false
	a.autoRecFile = nil
	a.autoRecPath = ""
	a.recMutex.Unlock()
}

func (a *App) resetDecoderLocked() {
	if a.decoder != nil {
		if a.packetCount >= 5 && !a.imageSaved {
			if jpg, ok := a.decoder.SnapshotJPEG(); ok {
				a.addHistoryEntry(jpg, a.lastImgID, a.packetCount, a.ssdvAPID)
				a.totalImages++
				a.imageSaved = true
			}
		}
		a.decoder.Close()
		a.decoder = nil
	}
	a.hasInit = false
	a.packetCount = 0
	a.imageSaved = false
	a.stopAutoRecording()
}

// maybeAddSnapshotToHistory adds/updates a partial decode snapshot in history at most once every 5s per image.
func (a *App) maybeAddSnapshotToHistory(jpg []byte, imgID uint16, pktCount int) {
	if jpg == nil {
		return
	}
	apid := a.ssdvAPID
	if apid == 0 {
		apid = 10
	}

	a.cfgMu.Lock()
	last, exists := a.lastHistoryAdd[imgID]
	if exists && time.Since(last) < 5*time.Second {
		a.cfgMu.Unlock()
		return
	}
	a.lastHistoryAdd[imgID] = time.Now()

	// Upsert: update existing entry or append new one, under same lock
	updated := false
	for i := range a.history {
		if a.history[i].ImgID == imgID && a.history[i].APID == apid {
			a.history[i].JPEG = jpg
			a.history[i].PacketCount = pktCount
			a.history[i].CompletedAt = time.Now()
			updated = true
			break
		}
	}
	if !updated {
		path, err := a.session.SaveImage(apid, imgID, jpg)
		if err != nil {
			log.Printf("Failed to save partial image: %v", err)
		}
		a.history = append(a.history, ImageHistoryEntry{
			JPEG: jpg, ImgID: imgID, CompletedAt: time.Now(),
			PacketCount: pktCount, APID: apid, SavedPath: path,
		})
	}
	if len(a.history) > a.cfg.MaxHistory {
		a.history = a.history[len(a.history)-a.cfg.MaxHistory:]
	}
	a.historyIdx = -1
	a.cfgMu.Unlock()
}

func (a *App) addHistoryEntry(jpg []byte, imgID uint16, pktCount int, apid int) {
	if jpg == nil {
		return
	}
	if apid == 0 {
		apid = 10
	}
	path, err := a.session.SaveImage(apid, imgID, jpg)
	if err != nil {
		log.Printf("Failed to save image: %v", err)
	}
	// Caller must hold a.cfgMu
	updated := false
	for i := range a.history {
		if a.history[i].ImgID == imgID && a.history[i].APID == apid {
			a.history[i].JPEG = jpg
			a.history[i].PacketCount = pktCount
			a.history[i].CompletedAt = time.Now()
			a.history[i].SavedPath = path
			updated = true
			break
		}
	}
	if !updated {
		a.history = append(a.history, ImageHistoryEntry{
			JPEG: jpg, ImgID: imgID, CompletedAt: time.Now(),
			PacketCount: pktCount, APID: apid, SavedPath: path,
		})
	}
	if len(a.history) > a.cfg.MaxHistory {
		a.history = a.history[len(a.history)-a.cfg.MaxHistory:]
	}
	a.historyIdx = -1
}

func (a *App) broadcastImage(jpg []byte, imgID uint16) {
	if jpg == nil {
		return
	}
	encoded := base64.StdEncoding.EncodeToString(jpg)
	runtime.EventsEmit(a.ctx, "newImage", encoded)
}
