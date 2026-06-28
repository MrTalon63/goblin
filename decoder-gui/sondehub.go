package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"time"
)

type SondehubTelemetry struct {
	SoftwareName     string    `json:"software_name"`
	SoftwareVersion  string    `json:"software_version"`
	UploaderCallsign string    `json:"uploader_callsign"`
	TimeReceived     string    `json:"time_received"`
	Datetime         string    `json:"datetime"`
	PayloadCallsign  string    `json:"payload_callsign"`
	Lat              float64   `json:"lat"`
	Lon              float64   `json:"lon"`
	Alt              float64   `json:"alt"`
	Sats             int       `json:"sats"`
	Batt             float64   `json:"batt"`
	Temp             float64   `json:"temp"`
	TempExt          float64   `json:"temp_ext"`
	UploaderPosition []float64 `json:"uploader_position,omitempty"`
	UploaderAntenna  string    `json:"uploader_antenna,omitempty"`
	UploaderRadio    string    `json:"uploader_radio,omitempty"`
}

func (a *App) uploadToSondeHub(data *CoreTelemetryData, cfg Config) {
	// Skip upload if callsign is empty, default N0CALL, or if uploader position is default [0,0]
	if cfg.SondehubUploaderCallsign == "" || cfg.SondehubUploaderCallsign == "N0CALL" {
		return
	}
	if cfg.SondehubUploaderLat == 0.0 && cfg.SondehubUploaderLon == 0.0 {
		return
	}

	payloadCallsign := data.Callsign
	if pName := a.session.PayloadName(); pName != "" {
		payloadCallsign = data.Callsign + "_" + pName
	}

	// Prepare payload (SondeHub expects a JSON array of telemetry records)
	record := SondehubTelemetry{
		SoftwareName:     "Goblin Decoder UI",
		SoftwareVersion:  Version,
		UploaderCallsign: cfg.SondehubUploaderCallsign,
		TimeReceived:     data.Received.UTC().Format("2006-01-02T15:04:05.000Z"),
		Datetime:         data.ComputedTime.UTC().Format("2006-01-02T15:04:05.000Z"),
		PayloadCallsign:  payloadCallsign,
		Lat:              data.Latitude,
		Lon:              data.Longitude,
		Alt:              float64(data.Altitude),
		Sats:             int(data.GPSSats),
		Batt:             data.BattVoltage,
		Temp:             data.TempInternal,
		TempExt:          data.TempExternal,
		UploaderPosition: []float64{cfg.SondehubUploaderLat, cfg.SondehubUploaderLon, cfg.SondehubUploaderAlt},
	}

	if cfg.SondehubUploaderAntenna != "" {
		record.UploaderAntenna = cfg.SondehubUploaderAntenna
	}
	if cfg.SondehubUploaderRadio != "" {
		record.UploaderRadio = cfg.SondehubUploaderRadio
	}

	payload := []SondehubTelemetry{record}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("SondeHub marshal error: %v", err)
		return
	}

	req, err := http.NewRequest("PUT", "https://api.v2.sondehub.org/amateur/telemetry", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("SondeHub request creation error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "goblin-decoder-gui/"+Version)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("SondeHub upload error: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("SondeHub rejected telemetry: status=%d response=%s", resp.StatusCode, string(bodyBytes))
	} else {
		log.Printf("SondeHub successfully uploaded telemetry packet for payload=%s", payloadCallsign)
	}
}
