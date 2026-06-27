package main

/*
#cgo CFLAGS: -Issdv-ng
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "ssdv.c"
#include "rs8.c"

typedef struct {
    ssdv_t dec;
    uint8_t *outbuf;
    size_t outcap;
} decoder_t;

decoder_t *new_decoder(int pkt_size) {
    decoder_t *d = (decoder_t *)malloc(sizeof(decoder_t));
    if (!d) return NULL;
    memset(d, 0, sizeof(decoder_t));
    if (ssdv_dec_init(&d->dec, pkt_size) != SSDV_OK) {
        free(d);
        return NULL;
    }
    d->outcap = 209715200; // 200MB
    d->outbuf = (uint8_t *)malloc(d->outcap);
    if (!d->outbuf) {
        free(d);
        return NULL;
    }
    ssdv_dec_set_buffer(&d->dec, d->outbuf, d->outcap);
    return d;
}

void free_decoder(decoder_t *d) {
    if (!d) return;
    if (d->outbuf) free(d->outbuf);
    free(d);
}

int feed_decoder(decoder_t *d, uint8_t *pkt, int pkt_size) {
    int r = ssdv_dec_feed(&d->dec, pkt);
    return r;
}

int get_jpeg(decoder_t *d, uint8_t **jpg, int *jpg_len) {
    size_t length;
    if (ssdv_dec_get_jpeg(&d->dec, jpg, &length) != SSDV_OK) {
        return -1;
    }
    *jpg_len = (int)length;
    return 1;
}

// Duplicate the decoder state (copy entire ssdv_t and output buffer)
decoder_t *duplicate_decoder(decoder_t *d) {
    decoder_t *copy = (decoder_t *)malloc(sizeof(decoder_t));
    if (!copy) return NULL;
    memcpy(copy, d, sizeof(decoder_t));
    // Guard against invalid pointer state
    if (d->dec.outp < d->dec.out) {
        free(copy);
        return NULL;
    }
    size_t used = d->dec.outp - d->dec.out;
    if (used > d->outcap) {
        free(copy);
        return NULL;
    }
    copy->outbuf = (uint8_t *)malloc(d->outcap);
    if (!copy->outbuf) {
        free(copy);
        return NULL;
    }
    memcpy(copy->outbuf, d->outbuf, used);
    // Adjust pointers in the copy
    copy->dec.out = copy->outbuf;
    copy->dec.outp = copy->outbuf + used;
    copy->dec.out_len = d->outcap - used; // remaining space
    return copy;
}

// Get JPEG from the duplicated decoder (finalizes it)
int get_jpeg_from_copy(decoder_t *copy, uint8_t **jpg, int *jpg_len) {
    size_t length;
    if (ssdv_dec_get_jpeg(&copy->dec, jpg, &length) != SSDV_OK) {
        return -1;
    }
    *jpg_len = (int)length;
    return 1;
}

void free_decoder_copy(decoder_t *copy) {
    if (copy) {
        if (copy->outbuf) free(copy->outbuf);
        free(copy);
    }
}

int is_packet_valid(uint8_t *pkt, int pkt_size) {
    int err = 0;
    return ssdv_dec_is_packet(pkt, pkt_size, &err) == 0;
}
*/
import "C"
import (
	"errors"
	"unsafe"
)

const (
	CCSDSPktSize = 246
	maxJPEGSize  = 10 * 1024 * 1024 // 10MB sanity cap
)

type Decoder struct {
	ptr *C.decoder_t
}

func NewDecoder(pktSize int) (*Decoder, error) {
	p := C.new_decoder(C.int(pktSize))
	if p == nil {
		return nil, errors.New("ssdv init failed")
	}
	return &Decoder{ptr: p}, nil
}

func (d *Decoder) Feed(pkt []byte) (int, error) {
	if len(pkt) == 0 {
		return C.SSDV_FEED_ME, nil
	}
	r := C.feed_decoder(d.ptr, (*C.uint8_t)(unsafe.Pointer(&pkt[0])), C.int(len(pkt)))
	if r == C.SSDV_ERROR {
		return int(r), errors.New("ssdv feed error")
	}
	return int(r), nil
}

func (d *Decoder) Width() uint16 {
	return uint16(d.ptr.dec.width)
}

func (d *Decoder) Height() uint16 {
	return uint16(d.ptr.dec.height)
}

func (d *Decoder) Quality() uint8 {
	return uint8(d.ptr.dec.quality)
}

func (d *Decoder) TryGetJPEG() ([]byte, bool) {
	var jpg *C.uint8_t
	var jpgLen C.int
	r := C.get_jpeg(d.ptr, &jpg, &jpgLen)
	if r != 1 || jpg == nil || jpgLen <= 0 {
		return nil, false
	}
	out := C.GoBytes(unsafe.Pointer(jpg), jpgLen)
	if len(out) > maxJPEGSize {
		return nil, false
	}
	return out, true
}

// SnapshotJPEG returns a valid JPEG snapshot without modifying the decoder state.
func (d *Decoder) SnapshotJPEG() ([]byte, bool) {
	// Duplicate the decoder
	copyPtr := C.duplicate_decoder(d.ptr)
	if copyPtr == nil {
		return nil, false
	}
	defer C.free_decoder_copy(copyPtr)

	var jpg *C.uint8_t
	var jpgLen C.int
	r := C.get_jpeg_from_copy(copyPtr, &jpg, &jpgLen)
	if r != 1 || jpg == nil || jpgLen <= 0 {
		return nil, false
	}
	out := C.GoBytes(unsafe.Pointer(jpg), jpgLen)
	if len(out) > maxJPEGSize {
		return nil, false
	}
	return out, true
}

func (d *Decoder) Close() {
	if d.ptr != nil {
		C.free_decoder(d.ptr)
		d.ptr = nil
	}
}

// Resolution returns the image width and height from the SSDV decoder header.
func (d *Decoder) Resolution() (width, height int, ok bool) {
	if d.ptr == nil {
		return 0, 0, false
	}
	w := int(d.ptr.dec.width)
	h := int(d.ptr.dec.height)
	if w > 0 && h > 0 {
		return w, h, true
	}
	return 0, 0, false
}

func IsPacketValid(pkt []byte, pktSize int) bool {
	if len(pkt) == 0 {
		return false
	}
	return C.is_packet_valid((*C.uint8_t)(unsafe.Pointer(&pkt[0])), C.int(pktSize)) == 0
}
