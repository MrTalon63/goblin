package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// APIDConfig defines the configuration for a single APID.
type APIDConfig struct {
	Port    uint16 `json:"port"`
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"` // "timing", "basic_telem", "dynamic_telem", "ssdv"
}

// APID type constants
const (
	APIDTypeTiming       = "timing"
	APIDTypeBasicTelem   = "basic_telem"
	APIDTypeDynamicTelem = "dynamic_telem"
	APIDTypeSSDV         = "ssdv"
)

type Config struct {
	APIDs                    map[int]APIDConfig `json:"apids"`
	CacheFile                string             `json:"cache_file"`
	WinWidth                 int                `json:"window_width"`
	WinHeight                int                `json:"window_height"`
	SaveDir                  string             `json:"save_dir"`
	MaxHistory               int                `json:"max_history"`
	TilePort                 int                `json:"tile_port"` // 0 = random
	TileDir                  string             `json:"tile_dir"`
	TileConcurrency          int                `json:"tile_concurrency"` // concurrent downloads (1-8)
	SondehubEnabled          bool               `json:"sondehub_enabled"`
	SondehubUploaderCallsign string             `json:"sondehub_uploader_callsign"`
	SondehubUploaderLat      float64            `json:"sondehub_uploader_lat"`
	SondehubUploaderLon      float64            `json:"sondehub_uploader_lon"`
	SondehubUploaderAlt      float64            `json:"sondehub_uploader_alt"`
	SondehubUploaderAntenna  string             `json:"sondehub_uploader_antenna"`
	SondehubUploaderRadio    string             `json:"sondehub_uploader_radio"`

	TelemetryServerEnabled bool    `json:"telemetry_server_enabled"`
	TelemetryServerUrl     string  `json:"telemetry_server_url"`
	TelemetryReceiverID    string  `json:"telemetry_receiver_id"`
	TelemetryNickname      string  `json:"telemetry_nickname"`
	TelemetryLat           float64 `json:"telemetry_lat"`
	TelemetryLon           float64 `json:"telemetry_lon"`
	TelemetryAlt           float64 `json:"telemetry_alt"`
	TelemetryAntenna       string  `json:"telemetry_antenna"`
	TelemetryRadio         string  `json:"telemetry_radio"`
	TelemetryPrompted      bool    `json:"telemetry_prompted"`
}

func DefaultConfig() Config {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, err = os.UserHomeDir()
		if err != nil {
			dir = "."
		}
	}
	baseDir := filepath.Join(dir, "goblin_decoder")
	return Config{
		APIDs: map[int]APIDConfig{
			0:  {Port: 9000, Enabled: true, Type: APIDTypeTiming},
			1:  {Port: 9001, Enabled: true, Type: APIDTypeBasicTelem},
			2:  {Port: 9002, Enabled: true, Type: APIDTypeDynamicTelem},
			10: {Port: 9010, Enabled: true, Type: APIDTypeSSDV},
			11: {Port: 9011, Enabled: true, Type: APIDTypeSSDV},
		},
		CacheFile:       filepath.Join(baseDir, "packets.ssdv"),
		WinWidth:        800,
		WinHeight:       600,
		SaveDir:         filepath.Join(baseDir, "data"),
		MaxHistory:      50,
		TileDir:         filepath.Join(baseDir, "tiles"),
		TilePort:        0,
		TileConcurrency: 2,
		SondehubEnabled:          false,
		SondehubUploaderCallsign: "N0CALL",
		SondehubUploaderLat:      0.0,
		SondehubUploaderLon:      0.0,
		SondehubUploaderAlt:      0.0,
		SondehubUploaderAntenna:  "",
		SondehubUploaderRadio:    "",

		TelemetryServerEnabled: false,
		TelemetryServerUrl:     "https://goblin.mrtalon.eu",
		TelemetryReceiverID:    "",
		TelemetryNickname:      "",
		TelemetryLat:           0.0,
		TelemetryLon:           0.0,
		TelemetryAlt:           0.0,
		TelemetryAntenna:       "",
		TelemetryRadio:         "",
		TelemetryPrompted:      false,
	}
}

func ConfigPath() string {
	if p := os.Getenv("GOBLIN_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		dir, err = os.UserHomeDir()
		if err != nil {
			dir = "."
		}
	}
	return filepath.Join(dir, "goblin_decoder", "config.json")
}

func LoadConfig() Config {
	p := ConfigPath()
	f, err := os.Open(p)
	if err != nil {
		return DefaultConfig()
	}
	defer f.Close()
	var c Config
	if err := json.NewDecoder(f).Decode(&c); err != nil {
		return DefaultConfig()
	}

	def := DefaultConfig()

	// Fill in missing APID configs from defaults
	if c.APIDs == nil {
		c.APIDs = def.APIDs
	} else {
		for k, v := range def.APIDs {
			if _, exists := c.APIDs[k]; !exists {
				c.APIDs[k] = v
			} else {
				existing := c.APIDs[k]
				if existing.Port == 0 {
					existing.Port = v.Port
				}
				if existing.Type == "" {
					existing.Type = v.Type
				}
				c.APIDs[k] = existing
			}
		}
	}

	if c.CacheFile == "" {
		c.CacheFile = def.CacheFile
	}
	if c.WinWidth == 0 {
		c.WinWidth = def.WinWidth
	}
	if c.WinHeight == 0 {
		c.WinHeight = def.WinHeight
	}
	if c.SaveDir == "" {
		c.SaveDir = def.SaveDir
	}
	if c.MaxHistory == 0 {
		c.MaxHistory = def.MaxHistory
	}
	if c.TileDir == "" {
		c.TileDir = def.TileDir
	}
	if c.TileConcurrency == 0 {
		c.TileConcurrency = def.TileConcurrency
	}
	if c.SondehubUploaderCallsign == "" {
		c.SondehubUploaderCallsign = def.SondehubUploaderCallsign
	}

	// Populate telemetry defaults
	if c.TelemetryServerUrl == "" {
		c.TelemetryServerUrl = def.TelemetryServerUrl
	}
	if c.TelemetryReceiverID == "" {
		c.TelemetryReceiverID = uuid.New().String()
		_ = SaveConfig(c) // Persist the generated UUID immediately
	}
	if c.TelemetryNickname == "" {
		c.TelemetryNickname = def.TelemetryNickname
	}

	return c
}

func SaveConfig(c Config) error {
	p := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	tmp := p + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(c); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	// Remove destination first on Windows (os.Rename fails if dest exists)
	os.Remove(p)
	return os.Rename(tmp, p)
}
