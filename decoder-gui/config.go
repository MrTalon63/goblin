package main

import (
	"encoding/json"
	"os"
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
	APIDs           map[int]APIDConfig `json:"apids"`
	CacheFile       string             `json:"cache_file"`
	WinWidth        int                `json:"window_width"`
	WinHeight       int                `json:"window_height"`
	SaveDir         string             `json:"save_dir"`
	MaxHistory      int                `json:"max_history"`
	TilePort        int                `json:"tile_port"` // 0 = random
	TileDir         string             `json:"tile_dir"`
	TileConcurrency int                `json:"tile_concurrency"` // concurrent downloads (1-8)
}

func DefaultConfig() Config {
	return Config{
		APIDs: map[int]APIDConfig{
			0:  {Port: 9000, Enabled: true, Type: APIDTypeTiming},
			1:  {Port: 9001, Enabled: true, Type: APIDTypeBasicTelem},
			2:  {Port: 9002, Enabled: true, Type: APIDTypeDynamicTelem},
			10: {Port: 9010, Enabled: true, Type: APIDTypeSSDV},
			11: {Port: 9011, Enabled: true, Type: APIDTypeSSDV},
		},
		CacheFile:       "packets.ssdv",
		WinWidth:        800,
		WinHeight:       600,
		SaveDir:         "data",
		MaxHistory:      50,
		TileDir:         "tiles",
		TilePort:        0,
		TileConcurrency: 2,
	}
}

func ConfigPath() string {
	if p := os.Getenv("GOBLIN_CONFIG"); p != "" {
		return p
	}
	return "config.json"
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
	return c
}

func SaveConfig(c Config) error {
	p := ConfigPath()
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
