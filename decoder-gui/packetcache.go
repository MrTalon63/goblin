package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
)

const (
	cacheMagic    = 0x53534456 // "SSDV"
	cacheVersion  = 1
	cacheMaxSize  = 25 * 1024 * 1024 // 25MB per file
	cacheMaxFiles = 3                // keep last 3 rotated files
)

type PacketRecord struct {
	ImageID  uint16
	PacketID uint32
	Payload  []byte
}

type PacketCache struct {
	mu       sync.Mutex
	file     *os.File
	basePath string
	sequence int
}

func NewPacketCache(path string) (*PacketCache, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	// Write header if file is empty
	stat, _ := f.Stat()
	if stat.Size() == 0 {
		var hdr [12]byte
		binary.BigEndian.PutUint32(hdr[0:4], cacheMagic)
		binary.BigEndian.PutUint32(hdr[4:8], cacheVersion)
		binary.BigEndian.PutUint32(hdr[8:12], 0)
		if _, err := f.Write(hdr[:]); err != nil {
			f.Close()
			return nil, err
		}
	}
	return &PacketCache{file: f, basePath: path}, nil
}

func (c *PacketCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file != nil {
		err := c.file.Close()
		c.file = nil
		return err
	}
	return nil
}

func (c *PacketCache) Write(r PacketRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.file == nil {
		return os.ErrClosed
	}
	// Rotate if file exceeds max size
	stat, _ := c.file.Stat()
	if stat.Size() >= cacheMaxSize {
		c.rotate()
	}
	var hdr [6]byte
	binary.BigEndian.PutUint16(hdr[0:2], r.ImageID)
	binary.BigEndian.PutUint32(hdr[2:6], r.PacketID)
	if _, err := c.file.Write(hdr[:]); err != nil {
		return err
	}
	if len(r.Payload) > 0xFFFF {
		return fmt.Errorf("payload too large: %d bytes (max 65535)", len(r.Payload))
	}
	var plen [2]byte
	binary.BigEndian.PutUint16(plen[:], uint16(len(r.Payload)))
	if _, err := c.file.Write(plen[:]); err != nil {
		return err
	}
	if _, err := c.file.Write(r.Payload); err != nil {
		return err
	}
	return nil
}

func (c *PacketCache) rotate() {
	if c.file != nil {
		c.file.Close()
		c.file = nil
	}
	// Delete oldest if we're at max
	if c.sequence >= cacheMaxFiles {
		oldPath := fmt.Sprintf("%s_%03d.ssdv", c.basePath, c.sequence-cacheMaxFiles+1)
		os.Remove(oldPath)
	}
	c.sequence++
	newPath := fmt.Sprintf("%s_%03d.ssdv", c.basePath, c.sequence)
	f, err := os.OpenFile(newPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	var hdr [12]byte
	binary.BigEndian.PutUint32(hdr[0:4], cacheMagic)
	binary.BigEndian.PutUint32(hdr[4:8], cacheVersion)
	binary.BigEndian.PutUint32(hdr[8:12], 0)
	f.Write(hdr[:])
	c.file = f
}
