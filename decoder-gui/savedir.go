package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SessionDir manages the data/<timestamp>[_<name>]/ directory structure.
type SessionDir struct {
	mu             sync.Mutex
	baseDir        string // path to "data" dir
	sessionDir     string // full path to current session dir, e.g. "data/20240101_120000"
	sessionName    string // placeholder timestamp name, e.g. "20240101_120000"
	payloadName    string // received from APID 2 "name" field
	hasPayloadName bool
	created        bool
}

// NewSessionDir creates a new session directory manager.
func NewSessionDir(baseDir string) *SessionDir {
	return &SessionDir{
		baseDir: baseDir,
	}
}

// IsCreated returns whether the session directory has been created on disk.
func (s *SessionDir) IsCreated() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created
}

// EnsureSession creates the session directory if it doesn't exist yet.
// Called on first packet received.
func (s *SessionDir) EnsureSession() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.created {
		return nil
	}

	// Create placeholder name: YYYYMMDD_HHMMSS
	now := time.Now()
	s.sessionName = now.Format("20060102_150405")
	s.sessionDir = filepath.Join(s.baseDir, s.sessionName)

	// Create subdirectories
	dirs := []string{
		s.sessionDir,
		filepath.Join(s.sessionDir, "images"),
		filepath.Join(s.sessionDir, "telemetry"),
		filepath.Join(s.sessionDir, "recordings"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create dir %s: %v", d, err)
		}
	}
	s.created = true
	log.Printf("Session directory created: %s", s.sessionDir)
	return nil
}

// SetPayloadName renames session dir to include payload name and moves all existing files.
func (s *SessionDir) SetPayloadName(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hasPayloadName && s.payloadName == name {
		return nil
	}
	if name == "" {
		s.payloadName = name
		s.hasPayloadName = true
		return nil
	}

	newName := s.sessionName + "_" + name
	newDir := filepath.Join(s.baseDir, newName)

	if s.created && s.sessionDir != newDir {
		// Move everything from old dir to new dir
		if err := os.MkdirAll(newDir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %v", newDir, err)
		}
		entries, err := os.ReadDir(s.sessionDir)
		if err == nil {
			for _, e := range entries {
				src := filepath.Join(s.sessionDir, e.Name())
				dst := filepath.Join(newDir, e.Name())
				if err := os.Rename(src, dst); err != nil {
					log.Printf("Move failed %s -> %s: %v", src, dst, err)
				}
			}
		}
		// Remove old empty dirs (best effort)
		os.Remove(filepath.Join(s.sessionDir, "recordings"))
		os.Remove(filepath.Join(s.sessionDir, "telemetry"))
		os.Remove(filepath.Join(s.sessionDir, "images"))
		os.Remove(s.sessionDir)
		s.sessionDir = newDir
		log.Printf("Session directory moved to: %s", newDir)
	} else if !s.created {
		// First time, just create named dir
		dirs := []string{newDir, filepath.Join(newDir, "images"), filepath.Join(newDir, "telemetry"), filepath.Join(newDir, "recordings")}
		for _, d := range dirs {
			if err := os.MkdirAll(d, 0755); err != nil {
				return fmt.Errorf("mkdir %s: %v", d, err)
			}
		}
		s.sessionDir = newDir
		s.created = true
		log.Printf("Session directory created: %s", newDir)
	}

	s.payloadName = name
	s.hasPayloadName = true
	return nil
}

// SessionPath returns the current session directory path.
func (s *SessionDir) SessionPath() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessionDir
}

// ImagesDir returns the images subdirectory path.
func (s *SessionDir) ImagesDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionDir == "" {
		return ""
	}
	return filepath.Join(s.sessionDir, "images")
}

// TelemetryDir returns the telemetry subdirectory path.
func (s *SessionDir) TelemetryDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionDir == "" {
		return ""
	}
	return filepath.Join(s.sessionDir, "telemetry")
}

// RecordingsDir returns the recordings subdirectory path.
func (s *SessionDir) RecordingsDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionDir == "" {
		return ""
	}
	return filepath.Join(s.sessionDir, "recordings")
}

// SaveImage saves a JPEG image to the session's images directory.
// filename format: <apid>/<imgID>.jpg
func (s *SessionDir) SaveImage(apid int, imgID uint16, data []byte) (string, error) {
	if err := s.EnsureSession(); err != nil {
		return "", err
	}
	apidDir := filepath.Join(s.ImagesDir(), fmt.Sprintf("%d", apid))
	if err := os.MkdirAll(apidDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create apid dir: %v", err)
	}
	fname := fmt.Sprintf("%d.jpg", imgID)
	path := filepath.Join(apidDir, fname)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("failed to save image %s: %v", fname, err)
	}
	log.Printf("Image saved: %s (%d bytes)", path, len(data))
	return path, nil
}

// SaveCSV appends a CSV row to the telemetry file for the given APID.
func (s *SessionDir) SaveCSV(apid int, apidType string, header string, row string) error {
	if err := s.EnsureSession(); err != nil {
		return err
	}

	fname := fmt.Sprintf("apid%d_%s.csv", apid, apidType)
	path := filepath.Join(s.TelemetryDir(), fname)

	// Write header if file doesn't exist
	_, err := os.Stat(path)
	writeHeader := os.IsNotExist(err)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open CSV %s: %v", fname, err)
	}
	defer f.Close()

	if writeHeader {
		if _, err := f.WriteString(header + "\n"); err != nil {
			return err
		}
	}
	if _, err := f.WriteString(row + "\n"); err != nil {
		return err
	}
	return nil
}

// OpenRecording opens (or creates) a recording file for the given APID in the recordings directory.
func (s *SessionDir) OpenRecording(apid int) (*os.File, error) {
	if err := s.EnsureSession(); err != nil {
		return nil, err
	}

	fname := fmt.Sprintf("apid%d.bin", apid)
	path := filepath.Join(s.RecordingsDir(), fname)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open recording %s: %v", fname, err)
	}
	return f, nil
}

// PayloadName returns the received payload name.
func (s *SessionDir) PayloadName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payloadName
}

