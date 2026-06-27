package main

import (
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	maxPacketsPerSec = 1000
	rateBucketSize   = maxPacketsPerSec
)

type UDPReceiver struct {
	conn     *net.UDPConn
	addr     *net.UDPAddr
	wg       sync.WaitGroup
	quit     chan struct{}
	onPacket func([]byte)

	// Rate limiting
	rateTokens float64
	rateLast   time.Time
}

func NewUDPReceiver(port uint16, handler func([]byte)) (*UDPReceiver, error) {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	r := &UDPReceiver{
		conn:       conn,
		addr:       addr,
		quit:       make(chan struct{}),
		onPacket:   handler,
		rateTokens: rateBucketSize,
		rateLast:   time.Now(),
	}
	r.wg.Add(1)
	go r.loop()
	return r, nil
}

func (r *UDPReceiver) allowPacket() bool {
	now := time.Now()
	elapsed := now.Sub(r.rateLast).Seconds()
	r.rateTokens += elapsed * maxPacketsPerSec
	r.rateLast = now
	if r.rateTokens > rateBucketSize {
		r.rateTokens = rateBucketSize
	}
	if r.rateTokens < 1 {
		return false
	}
	r.rateTokens--
	return true
}

func (r *UDPReceiver) loop() {
	defer r.wg.Done()
	buf := make([]byte, 4096)
	for {
		select {
		case <-r.quit:
			return
		default:
		}
		r.conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, _, err := r.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			select {
			case <-r.quit:
				return
			default:
				continue
			}
		}
		if n > 0 && r.onPacket != nil {
			if !r.allowPacket() {
				continue // rate limit exceeded, drop packet
			}
			pkt := make([]byte, n)
			copy(pkt, buf[:n])
			r.onPacket(pkt)
		}
	}
}

func (r *UDPReceiver) Close() error {
	close(r.quit)
	r.conn.Close()
	r.wg.Wait()
	return nil
}
