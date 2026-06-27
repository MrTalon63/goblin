package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// startAllReceivers starts UDP receivers for all enabled APIDs.
func (a *App) startAllReceivers() {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	a.startAllReceiversLocked()
}

func (a *App) startAllReceiversLocked() {
	for apid, apidCfg := range a.cfg.APIDs {
		if !apidCfg.Enabled {
			continue
		}
		a.startAPIDReceiverLocked(apid, apidCfg)
	}
}

func (a *App) startAPIDReceiverLocked(apid int, apidCfg APIDConfig) {
	handler := func(pkt []byte) {
		a.handleAPIDPacket(apid, apidCfg.Type, pkt)
	}
	r, err := NewUDPReceiver(apidCfg.Port, handler)
	if err != nil {
		log.Printf("Failed to start UDP receiver for APID %d on port %d: %v", apid, apidCfg.Port, err)
		return
	}
	a.udpReceivers[apid] = r
	log.Printf("UDP receiver started for APID %d (%s) on port %d", apid, apidCfg.Type, apidCfg.Port)
	runtime.LogInfof(a.ctx, "UDP receiver started for APID %d on port %d", apid, apidCfg.Port)
}

func (a *App) handleAPIDPacket(apid int, apidType string, pkt []byte) {
	a.apidMu.Lock()
	if a.firstPacketTime.IsZero() {
		a.firstPacketTime = time.Now()
	}
	a.apidCounters[apid]++
	a.apidMu.Unlock()

	a.recMutex.Lock()
	f, ok := a.recordingFiles[apid]
	if !ok {
		var err error
		f, err = a.session.OpenRecording(apid)
		if err != nil {
			log.Printf("Failed to open recording for APID %d: %v", apid, err)
		} else {
			a.recordingFiles[apid] = f
		}
	}
	if f != nil {
		f.Write(pkt)
		f.Sync()
	}
	if a.recording && a.recFile != nil {
		a.recFile.Write(pkt)
		a.recFile.Sync()
	}
	a.recMutex.Unlock()

	switch apidType {
	case APIDTypeTiming:
		a.handleAPID0(apid, pkt)
	case APIDTypeBasicTelem:
		a.handleAPID1(apid, pkt)
	case APIDTypeDynamicTelem:
		a.handleAPID2(apid, pkt)
	case APIDTypeSSDV:
		a.handleAPIDSSDV(apid, pkt)
	}
}

func (a *App) handleAPID0(apid int, pkt []byte) {
	data, err := DecodeAPID0(pkt)
	if err != nil {
		msg := fmt.Sprintf("APID 0 decode error: %v", err)
		log.Print(msg)
		runtime.EventsEmit(a.ctx, "log", msg)
		return
	}
	a.telemMutex.Lock()
	a.telemTimesync = data
	a.telemMutex.Unlock()
	a.session.SaveCSV(apid, "timesync", data.CSVHeader(), data.CSVRow())
	msg := fmt.Sprintf("APID 0: timesync OK")
	runtime.EventsEmit(a.ctx, "log", msg)
}

func (a *App) handleAPID1(apid int, pkt []byte) {
	data, err := DecodeAPID1(pkt)
	if err != nil {
		msg := fmt.Sprintf("APID 1 decode error: %v", err)
		log.Print(msg)
		runtime.EventsEmit(a.ctx, "log", msg)
		return
	}
	a.telemMutex.Lock()
	base := a.telemTimesync
	if base != nil {
		data.ComputedTime = ComputeAPID1Time(base, data.TimeOffset)
	}
	a.telemCore = data
	a.telemMutex.Unlock()
	a.session.SaveCSV(apid, "basic_telem", data.CSVHeader(), data.CSVRow())

	// Notify frontend
	a.broadcastTelemetry()
}

func (a *App) handleAPID2(apid int, pkt []byte) {
	data, err := DecodeAPID2(pkt)
	if err != nil {
		msg := fmt.Sprintf("APID 2 decode error: %v", err)
		log.Print(msg)
		runtime.EventsEmit(a.ctx, "log", msg)
		return
	}
	a.telemMutex.Lock()
	base := a.telemTimesync
	if base != nil {
		data.ComputedTime = ComputeAPID2Time(base, data.Raw)
	}
	a.telemDynamic = data
	a.telemMutex.Unlock()

	if data.Name != "" {
		// Close all open recording handles so Windows allows the dir rename/move
		a.stopAutoRecording()
		a.recMutex.Lock()
		for _, f := range a.recordingFiles {
			if f != nil {
				f.Close()
			}
		}
		a.recordingFiles = make(map[int]*os.File)
		a.recMutex.Unlock()

		if err := a.session.SetPayloadName(data.Name); err != nil {
			log.Printf("SetPayloadName error: %v", err)
		}

		// Reopen recordings — they now point to the renamed dir
		a.cfgMu.Lock()
		var enabledAPIDs []int
		for apid, apidCfg := range a.cfg.APIDs {
			if apidCfg.Enabled {
				enabledAPIDs = append(enabledAPIDs, apid)
			}
		}
		a.cfgMu.Unlock()

		a.recMutex.Lock()
		for _, apid := range enabledAPIDs {
			if f, err := a.session.OpenRecording(apid); err == nil {
				a.recordingFiles[apid] = f
			} else {
				log.Printf("Failed to reopen recording for APID %d: %v", apid, err)
			}
		}
		a.recMutex.Unlock()
	}
	a.session.SaveCSV(apid, "dynamic_telem", data.CSVHeader(), data.CSVRow())
	a.broadcastTelemetry()
}

func (a *App) handleAPIDSSDV(apid int, pkt []byte) {
	if len(pkt) < 14 {
		msg := fmt.Sprintf("SSDV packet too short: %d bytes", len(pkt))
		log.Print(msg)
		runtime.EventsEmit(a.ctx, "log", msg)
		a.cfgMu.Lock()
		a.corruptCount++
		a.cfgMu.Unlock()
		return
	}
	if len(pkt) > CCSDSPktSize {
		pkt = pkt[:CCSDSPktSize]
	} else if len(pkt) < CCSDSPktSize {
		padded := make([]byte, CCSDSPktSize)
		copy(padded, pkt)
		pkt = padded
	}
	imgID := imageIDFromCCSDSPayload(pkt)
	packetID := packetIDFromCCSDSPayload(pkt)
	eoi := eoiFromCCSDSPayload(pkt)

	a.cfgMu.Lock()
	key := uint64(imgID)<<32 | uint64(packetID)
	if _, exists := a.seenPackets[key]; exists {
		a.cfgMu.Unlock()
		return
	}
	if len(a.seenPackets) >= 100000 {
		var keys []uint64
		for k := range a.seenPackets {
			keys = append(keys, k)
			if len(keys) >= 50000 {
				break
			}
		}
		for _, k := range keys {
			delete(a.seenPackets, k)
		}
	}
	a.seenPackets[key] = true
	a.ssdvAPID = apid
	a.cfgMu.Unlock()

	a.recMutex.Lock()
	if a.cache != nil {
		a.cache.Write(PacketRecord{ImageID: imgID, PacketID: packetID, Payload: pkt})
	}
	a.recMutex.Unlock()

	a.cfgMu.Lock()
	a.lastPktTime = time.Now()

	if !a.hasInit || imgID != a.lastImgID {
		if a.hasInit && a.decoder != nil {
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
		a.startAutoRecording(imgID)
		d, err := NewDecoder(CCSDSPktSize)
		if err != nil {
			a.cfgMu.Unlock()
			log.Printf("Decoder creation failed: %v", err)
			return
		}
		a.decoder = d
		a.lastImgID = imgID
		a.packetCount = 0
		a.hasInit = true
		a.imageSaved = false
		a.seenPackets = make(map[uint64]bool)
	}
	dec := a.decoder
	a.cfgMu.Unlock()

	if dec == nil {
		return
	}

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC feeding packet %d: %v", packetID, r)
				a.cfgMu.Lock()
				a.corruptCount++
				a.cfgMu.Unlock()
			}
		}()
		_, err := dec.Feed(pkt)
		if err != nil {
			log.Printf("Feed error pkt %d: %v", packetID, err)
			a.cfgMu.Lock()
			a.corruptCount++
			a.cfgMu.Unlock()
		} else {
			a.cfgMu.Lock()
			a.packetCount++
			a.cfgMu.Unlock()
			if jpg, ok := dec.SnapshotJPEG(); ok {
				a.snapshotMu.Lock()
				a.snapshotData = jpg
				a.snapshotID = imgID
				a.snapshotPkts = a.packetCount
				a.snapshotEOI = eoi
				a.hasSnapshot = true
				a.snapshotMu.Unlock()
			}
			if eoi {
				if jpg, ok := dec.TryGetJPEG(); ok {
					a.cfgMu.Lock()
					a.updateImage(jpg, imgID, true, a.packetCount)
					a.addHistoryEntry(jpg, imgID, a.packetCount, a.ssdvAPID)
					a.totalImages++
					a.imageSaved = true
					a.hasInit = false
					a.packetCount = 0
					a.cfgMu.Unlock()
					a.stopAutoRecording()
					// Notify frontend about new image
					a.broadcastImage(jpg, imgID)
				}
			}
		}
	}()
}
