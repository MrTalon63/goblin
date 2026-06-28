package main

import (
	"log"
	urlpkg "net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type CentralUploader struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	cfg      Config
	sendChan chan interface{}
	stopChan chan struct{}
	running  bool
}

func NewCentralUploader() *CentralUploader {
	return &CentralUploader{
		sendChan: make(chan interface{}, 200),
	}
}

func (u *CentralUploader) Start(cfg Config) {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.running {
		u.stopUnlocked()
	}

	u.cfg = cfg
	if !cfg.TelemetryServerEnabled {
		return
	}

	u.sendChan = make(chan interface{}, 200)
	u.stopChan = make(chan struct{})
	u.running = true

	go u.runLoop()
}

func (u *CentralUploader) Stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stopUnlocked()
}

func (u *CentralUploader) stopUnlocked() {
	if !u.running {
		return
	}
	u.running = false
	close(u.stopChan)
	if u.conn != nil {
		u.conn.Close()
		u.conn = nil
	}
}

func (u *CentralUploader) Send(msg interface{}) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.running {
		return
	}
	select {
	case u.sendChan <- msg:
	default:
		// Queue full, drop packet
	}
}

func (u *CentralUploader) runLoop() {
	var err error

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	connect := func() bool {
		u.mu.Lock()
		if !u.running {
			u.mu.Unlock()
			return false
		}
		url := u.cfg.TelemetryServerUrl
		u.mu.Unlock()

		if url == "" {
			return false
		}

		// Convert http(s) to ws(s) if provided
		if strings.HasPrefix(url, "https://") {
			url = "wss://" + strings.TrimPrefix(url, "https://")
		} else if strings.HasPrefix(url, "http://") {
			url = "ws://" + strings.TrimPrefix(url, "http://")
		} else if !strings.HasPrefix(url, "ws://") && !strings.HasPrefix(url, "wss://") {
			// default to wss if no protocol specified
			url = "wss://" + url
		}

		// Ensure it has /ws path if not specified
		parsedURL, err := urlpkg.Parse(url)
		if err == nil {
			if parsedURL.Path == "" || parsedURL.Path == "/" {
				parsedURL.Path = "/ws"
			}
			url = parsedURL.String()
		}

		c, _, err := websocket.DefaultDialer.Dial(url, nil)
		if err != nil {
			log.Printf("[CentralUploader] Connection error to %s: %v", url, err)
			return false
		}

		// Register the receiver
		u.mu.Lock()
		if !u.running {
			u.mu.Unlock()
			c.Close()
			return false
		}
		regMsg := map[string]interface{}{
			"type":        "register",
			"receiver_id": u.cfg.TelemetryReceiverID,
			"nickname":    u.cfg.TelemetryNickname,
			"lat":         u.cfg.TelemetryLat,
			"lon":         u.cfg.TelemetryLon,
			"alt":         u.cfg.TelemetryAlt,
			"antenna":     u.cfg.TelemetryAntenna,
			"radio":       u.cfg.TelemetryRadio,
		}
		u.mu.Unlock()

		err = c.WriteJSON(regMsg)
		if err != nil {
			log.Printf("[CentralUploader] Registration write failed: %v", err)
			c.Close()
			return false
		}

		u.mu.Lock()
		if !u.running {
			u.mu.Unlock()
			c.Close()
			return false
		}
		u.conn = c
		u.mu.Unlock()

		log.Printf("[CentralUploader] Connected & registered successfully to %s", url)
		return true
	}

	// Try initial connection
	connected := connect()

	for {
		select {
		case <-u.stopChan:
			return

		case msg := <-u.sendChan:
			if !connected {
				continue
			}

			u.mu.Lock()
			c := u.conn
			u.mu.Unlock()

			if c != nil {
				err = c.WriteJSON(msg)
				if err != nil {
					log.Printf("[CentralUploader] Send failed, disconnecting: %v", err)
					u.mu.Lock()
					if u.conn == c {
						u.conn.Close()
						u.conn = nil
					}
					u.mu.Unlock()
					connected = false
				}
			}

		case <-ticker.C:
			if !connected {
				connected = connect()
			} else {
				// Ping connection to keep it alive
				u.mu.Lock()
				c := u.conn
				u.mu.Unlock()

				if c != nil {
					err = c.WriteMessage(websocket.PingMessage, []byte{})
					if err != nil {
						log.Printf("[CentralUploader] Ping failed, disconnecting: %v", err)
						u.mu.Lock()
						if u.conn == c {
							u.conn.Close()
							u.conn = nil
						}
						u.mu.Unlock()
						connected = false
					}
				}
			}
		}
	}
}
