package main

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// ComputeAPID1Time computes absolute wall-clock time from APID 0 time sync reference and APID 1 time offset.
func ComputeAPID1Time(base *TimeSyncData, offset uint16) time.Time {
	if base == nil {
		return time.Time{}
	}
	// offset is a wrapping 16-bit counter of 10ms ticks.
	// We compute the signed 16-bit delta between the offset and base.BaseTicks.
	diff := int16(offset - uint16(base.BaseTicks))
	offsetSec := float64(diff) / 100.0
	return time.Unix(int64(base.BaseTime), 0).Add(time.Duration(offsetSec * float64(time.Second)))
}

// ComputeAPID2Time computes absolute wall-clock time for APID 2 when CBOR contains integer "offset".
func ComputeAPID2Time(base *TimeSyncData, raw map[string]interface{}) time.Time {
	if base == nil {
		return time.Time{}
	}
	var offset float64
	switch v := raw["offset"].(type) {
	case float64:
		offset = v
	case int64:
		offset = float64(v)
	case uint64:
		offset = float64(v)
	default:
		return time.Time{}
	}
	// offset is a wrapping 16-bit counter of 10ms ticks.
	diff := int16(uint16(offset) - uint16(base.BaseTicks))
	offsetSec := float64(diff) / 100.0
	return time.Unix(int64(base.BaseTime), 0).Add(time.Duration(offsetSec * float64(time.Second)))
}

// ---------- APID 0: Time Sync (UPER) ----------

type TimeSyncData struct {
	BaseTime  uint32 // UNIX seconds
	BaseTicks uint32 // system uptime in 10ms ticks
	Received  time.Time
}

// DecodeAPID0 decodes a raw UPER-encoded APID 0 time sync packet.
// Fields: baseTime (32 bits) + baseTicks (32 bits) = 64 bits = 8 bytes.
func DecodeAPID0(pkt []byte) (*TimeSyncData, error) {
	if len(pkt) != 8 {
		return nil, fmt.Errorf("APID0 packet size invalid: got %d bytes, expected 8", len(pkt))
	}
	// UPER for simple 32-bit integers uses big-endian directly
	baseTime := binary.BigEndian.Uint32(pkt[0:4])
	baseTicks := binary.BigEndian.Uint32(pkt[4:8])
	return &TimeSyncData{
		BaseTime:  baseTime,
		BaseTicks: baseTicks,
		Received:  time.Now(),
	}, nil
}

func (t *TimeSyncData) CSVHeader() string {
	return "received_at,base_time_unix,base_time_iso,base_ticks"
}

func (t *TimeSyncData) CSVRow() string {
	iso := time.Unix(int64(t.BaseTime), 0).UTC().Format(time.RFC3339)
	return fmt.Sprintf("%s,%d,%s,%d",
		t.Received.Format(time.RFC3339Nano),
		t.BaseTime, iso, t.BaseTicks)
}

func (t *TimeSyncData) DisplayLines() []string {
	iso := time.Unix(int64(t.BaseTime), 0).UTC().Format(time.RFC3339)
	return []string{
		fmt.Sprintf("Base Time: %d (%s)", t.BaseTime, iso),
		fmt.Sprintf("Base Ticks: %d (10ms units = %.1fs)", t.BaseTicks, float64(t.BaseTicks)/100.0),
	}
}

// ---------- APID 1: Core Telemetry (UPER) ----------

type CoreTelemetryData struct {
	Callsign     string
	TimeOffset   uint16  // 10ms resolution
	Latitude     float64 // degrees
	Longitude    float64 // degrees
	Altitude     uint16  // meters
	GPSSats      uint8
	GPSLock      string  // "none", "2D", "3D", "diff"
	BattVoltage  float64 // volts
	TempInternal float64 // °C
	TempExternal float64 // °C
	ComputedTime time.Time
	Received     time.Time
}

// bitReader helps read bits from a byte buffer for UPER decoding.
type bitReader struct {
	data []byte
	pos  int // bit position
}

func newBitReader(data []byte) *bitReader {
	return &bitReader{data: data, pos: 0}
}

func (br *bitReader) readBits(n int) (uint64, error) {
	if n < 0 || n > 64 {
		return 0, fmt.Errorf("invalid bit count: %d", n)
	}
	if br.pos+n > len(br.data)*8 {
		return 0, fmt.Errorf("bit reader overflow: need %d bits at pos %d, have %d", n, br.pos, len(br.data)*8)
	}
	var result uint64
	for i := 0; i < n; i++ {
		byteIdx := br.pos / 8
		bitIdx := 7 - (br.pos % 8) // UPER is MSB-first
		if br.data[byteIdx]&(1<<bitIdx) != 0 {
			result |= 1 << (uint(n) - 1 - uint(i))
		}
		br.pos++
	}
	return result, nil
}

// DecodeAPID1 decodes a raw UPER-encoded APID 1 core telemetry packet.
// ASN.1 schema defines these fields in order:
//
//	callsign: SIZE (4..6) × 6-bit chars (A-Z=0-25, 0-9=26-35), with 2-bit length determinant
//	timeOffset: 16 bits
//	latitude: 31 bits (-900000000..900000000 → signed)
//	longitude: 32 bits (-1800000000..1800000000 → signed)
//	altitude: 16 bits (0..65535)
//	gpsSats: 5 bits (0..31)
//	gpsLock: 2 bits (enum 0-3)
//	battVoltage: 12 bits (0..3000)
//	tempInternal: 11 bits (-500..1000 → signed)
//	tempExternal: 11 bits (-1000..500 → signed)
func DecodeAPID1(pkt []byte) (*CoreTelemetryData, error) {
	// APID1-CoreTelemetry contains 162 to 174 bits (21 to 22 bytes) depending on callsign length
	if len(pkt) < 21 || len(pkt) > 22 {
		return nil, fmt.Errorf("APID1 packet size invalid: got %d bytes, expected 21 or 22", len(pkt))
	}

	br := newBitReader(pkt)

	// Callsign: SIZE (4..6) → 2-bit length determinant, then n × 6-bit chars
	const charset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	callsignLenRaw, err := br.readBits(2)
	if err != nil {
		return nil, fmt.Errorf("callsign length: %v", err)
	}
	callsignLen := int(callsignLenRaw) + 4 // 0→4, 1→5, 2→6
	callsign := make([]byte, callsignLen)
	for i := 0; i < int(callsignLen); i++ {
		v, err := br.readBits(6)
		if err != nil {
			return nil, fmt.Errorf("callsign char %d: %v", i, err)
		}
		if int(v) < len(charset) {
			callsign[i] = charset[v]
		} else {
			callsign[i] = ' '
		}
	}

	// timeOffset: 16 bits
	timeOffset, err := br.readBits(16)
	if err != nil {
		return nil, fmt.Errorf("timeOffset: %v", err)
	}

	// latitude: 31 bits, offset binary (range -90M..+90M)
	latRaw, err := br.readBits(31)
	if err != nil {
		return nil, fmt.Errorf("latitude: %v", err)
	}
	latitude := float64(int64(latRaw)-900000000) / 1e7

	// longitude: 32 bits, offset binary (range -1800M..+1800M)
	lonRaw, err := br.readBits(32)
	if err != nil {
		return nil, fmt.Errorf("longitude: %v", err)
	}
	longitude := float64(int64(lonRaw)-1800000000) / 1e7

	// altitude: 16 bits
	altitude, err := br.readBits(16)
	if err != nil {
		return nil, fmt.Errorf("altitude: %v", err)
	}

	// gpsSats: 5 bits
	gpsSats, err := br.readBits(5)
	if err != nil {
		return nil, fmt.Errorf("gpsSats: %v", err)
	}

	// gpsLock: 2 bits
	gpsLockRaw, err := br.readBits(2)
	if err != nil {
		return nil, fmt.Errorf("gpsLock: %v", err)
	}
	gpsLockNames := []string{"none", "2D", "3D", "diff"}
	gpsLock := "unknown"
	if int(gpsLockRaw) < len(gpsLockNames) {
		gpsLock = gpsLockNames[gpsLockRaw]
	}

	// battVoltage: 12 bits
	battRaw, err := br.readBits(12)
	if err != nil {
		return nil, fmt.Errorf("battVoltage: %v", err)
	}
	battVoltage := float64(battRaw) / 100.0

	// tempInternal: 11 bits, offset binary (range -500..+1000)
	tempIntRaw, err := br.readBits(11)
	if err != nil {
		return nil, fmt.Errorf("tempInternal: %v", err)
	}
	tempInternal := float64(int64(tempIntRaw)-500) / 10.0

	// tempExternal: 11 bits, offset binary (range -1000..+500)
	tempExtRaw, err := br.readBits(11)
	if err != nil {
		return nil, fmt.Errorf("tempExternal: %v", err)
	}
	tempExternal := float64(int64(tempExtRaw)-1000) / 10.0

	return &CoreTelemetryData{
		Callsign:     string(callsign),
		TimeOffset:   uint16(timeOffset),
		Latitude:     latitude,
		Longitude:    longitude,
		Altitude:     uint16(altitude),
		GPSSats:      uint8(gpsSats),
		GPSLock:      gpsLock,
		BattVoltage:  battVoltage,
		TempInternal: tempInternal,
		TempExternal: tempExternal,
		Received:     time.Now(),
	}, nil
}

func (t *CoreTelemetryData) CSVHeader() string {
	return "received_at,callsign,computed_time,time_offset_10ms,latitude,longitude,altitude_m,gps_sats,gps_lock,batt_voltage,temp_internal_c,temp_external_c"
}

func (t *CoreTelemetryData) CSVRow() string {
	computed := ""
	if !t.ComputedTime.IsZero() {
		computed = t.ComputedTime.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s,%s,%s,%d,%.7f,%.7f,%d,%d,%s,%.2f,%.1f,%.1f",
		t.Received.Format(time.RFC3339Nano),
		t.Callsign, computed, t.TimeOffset,
		t.Latitude, t.Longitude,
		t.Altitude, t.GPSSats, t.GPSLock,
		t.BattVoltage, t.TempInternal, t.TempExternal)
}

func (t *CoreTelemetryData) DisplayLines() []string {
	lines := []string{
		fmt.Sprintf("Callsign: %s", t.Callsign),
		fmt.Sprintf("Time Offset: %d (%.1fs)", t.TimeOffset, float64(t.TimeOffset)/100.0),
		fmt.Sprintf("Lat/Lon: %.5f, %.5f", t.Latitude, t.Longitude),
		fmt.Sprintf("Altitude: %d m", t.Altitude),
		fmt.Sprintf("GPS: %d sats, lock=%s", t.GPSSats, t.GPSLock),
		fmt.Sprintf("Battery: %.2f V", t.BattVoltage),
		fmt.Sprintf("Temp Int: %.1f °C, Ext: %.1f °C", t.TempInternal, t.TempExternal),
	}
	if !t.ComputedTime.IsZero() {
		lines = append(lines, fmt.Sprintf("Computed Time: %s", t.ComputedTime.Format(time.RFC3339)))
	} else {
		lines = append(lines, "Computed Time: not synced")
	}
	return lines
}

// ---------- APID 2: Dynamic Telemetry (CBOR) ----------

type DynamicTelemetryData struct {
	Name         string                 // "name" field at JSON root
	Raw          map[string]interface{} // full decoded CBOR payload
	RawBytes     []byte                 // original raw bytes
	ComputedTime time.Time
	Received     time.Time
}

// DecodeAPID2 decodes a CBOR-encoded APID 2 dynamic telemetry packet.
func DecodeAPID2(pkt []byte) (*DynamicTelemetryData, error) {
	if len(pkt) == 0 {
		return nil, fmt.Errorf("APID2 empty packet")
	}

	var result map[string]interface{}
	if err := cbor.Unmarshal(pkt, &result); err != nil {
		return nil, fmt.Errorf("CBOR decode failed: %v", err)
	}

	name := ""
	if n, ok := result["name"]; ok {
		if s, ok := n.(string); ok {
			name = s
		}
	}

	return &DynamicTelemetryData{
		Name:     name,
		Raw:      result,
		RawBytes: pkt,
		Received: time.Now(),
	}, nil
}

func (t *DynamicTelemetryData) CSVHeader() string {
	return "received_at,name,computed_time,payload_json"
}

func (t *DynamicTelemetryData) CSVRow() string {
	// We use JSON Lines format for dynamic telemetry (one JSON object per line)
	// Since it's CBOR, we'll just record it as the raw JSON representation
	jsonStr := "{ \"raw_hex\": \""
	for _, b := range t.RawBytes {
		jsonStr += fmt.Sprintf("%02x", b)
	}
	jsonStr += "\" }"
	computed := ""
	if !t.ComputedTime.IsZero() {
		computed = t.ComputedTime.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%s,%s,%s,%s",
		t.Received.Format(time.RFC3339Nano),
		t.Name, computed, jsonStr)
}

func (t *DynamicTelemetryData) DisplayLines() []string {
	lines := []string{
		fmt.Sprintf("Name: %s", t.Name),
		fmt.Sprintf("Payload Size: %d bytes", len(t.RawBytes)),
	}
	if !t.ComputedTime.IsZero() {
		lines = append(lines, fmt.Sprintf("Computed Time: %s", t.ComputedTime.Format(time.RFC3339)))
	} else {
		lines = append(lines, "Computed Time: not synced")
	}
	for k, v := range t.Raw {
		lines = append(lines, fmt.Sprintf("  %s: %v", k, v))
	}
	return lines
}
