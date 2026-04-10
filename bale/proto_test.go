package bale

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPbWriterVarint(t *testing.T) {
	tests := []struct {
		value    uint32
		expected []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7F}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xAC, 0x02}},
		{16384, []byte{0x80, 0x80, 0x01}},
	}
	for _, tc := range tests {
		w := NewPbWriter()
		w.WriteVarint(tc.value)
		if !bytes.Equal(w.Finish(), tc.expected) {
			t.Errorf("WriteVarint(%d) = %x, want %x", tc.value, w.Finish(), tc.expected)
		}
	}
}

func TestPbReaderVarint(t *testing.T) {
	tests := []struct {
		input    []byte
		expected uint32
	}{
		{[]byte{0x00}, 0},
		{[]byte{0x01}, 1},
		{[]byte{0x7F}, 127},
		{[]byte{0x80, 0x01}, 128},
		{[]byte{0xAC, 0x02}, 300},
	}
	for _, tc := range tests {
		r := NewPbReader(tc.input)
		v, err := r.ReadVarint()
		if err != nil {
			t.Errorf("ReadVarint(%x) error: %v", tc.input, err)
			continue
		}
		if v != tc.expected {
			t.Errorf("ReadVarint(%x) = %d, want %d", tc.input, v, tc.expected)
		}
	}
}

func TestHandshakeRoundTrip(t *testing.T) {
	// Encode a handshake request
	hsReq := EncodeHandshakeRequest()
	if len(hsReq) == 0 {
		t.Fatal("EncodeHandshakeRequest produced empty output")
	}

	// Decode it as a ClientEnvelope
	env, err := DecodeClientEnvelope(hsReq)
	if err != nil {
		t.Fatalf("DecodeClientEnvelope error: %v", err)
	}
	if env.Type != "handshake" {
		t.Errorf("Expected type 'handshake', got '%s'", env.Type)
	}
	if env.HandshakeRequest == nil {
		t.Fatal("HandshakeRequest is nil")
	}

	// Encode a handshake response
	hsResp := EncodeHandshakeResponse()
	if len(hsResp) == 0 {
		t.Fatal("EncodeHandshakeResponse produced empty output")
	}

	// Decode it as a ServerEnvelope
	senv, err := DecodeServerEnvelope(hsResp)
	if err != nil {
		t.Fatalf("DecodeServerEnvelope error: %v", err)
	}
	if senv.Type != "handshake" {
		t.Errorf("Expected type 'handshake', got '%s'", senv.Type)
	}
}

func TestPingPongRoundTrip(t *testing.T) {
	// Encode ping
	ping := EncodePing(42)
	env, err := DecodeClientEnvelope(ping)
	if err != nil {
		t.Fatalf("DecodeClientEnvelope(ping) error: %v", err)
	}
	if env.Type != "ping" {
		t.Errorf("Expected type 'ping', got '%s'", env.Type)
	}
	id, err := DecodePing(env.Ping)
	if err != nil {
		t.Fatalf("DecodePing error: %v", err)
	}
	if id != 42 {
		t.Errorf("Ping ID = %d, want 42", id)
	}

	// Encode pong
	pong := EncodePong(42)
	senv, err := DecodeServerEnvelope(pong)
	if err != nil {
		t.Fatalf("DecodeServerEnvelope(pong) error: %v", err)
	}
	if senv.Type != "pong" {
		t.Errorf("Expected type 'pong', got '%s'", senv.Type)
	}
}

func TestWrapUnwrapTunnelData(t *testing.T) {
	// Simulate VLESS tunnel data
	tunnelData := []byte("VLESS tunnel payload with binary \x00\xFE\xFF data")

	// Wrap on client side
	wrapped := WrapTunnelData(tunnelData, 1)
	if len(wrapped) == 0 {
		t.Fatal("WrapTunnelData produced empty output")
	}

	// Decode as ClientEnvelope (what the Worker does)
	env, err := DecodeClientEnvelope(wrapped)
	if err != nil {
		t.Fatalf("DecodeClientEnvelope error: %v", err)
	}
	if env.Type != "request" {
		t.Fatalf("Expected type 'request', got '%s'", env.Type)
	}

	// Decode the Request inside
	req, err := DecodeRequest(env.Request)
	if err != nil {
		t.Fatalf("DecodeRequest error: %v", err)
	}

	// Verify service/method are from Bale's list
	if req.ServiceName != Services[1%len(Services)] {
		t.Errorf("ServiceName = '%s', expected '%s'", req.ServiceName, Services[1%len(Services)])
	}

	// Strip padding to get original data
	clean := StripPadding(req.Payload)
	if !bytes.Equal(clean, tunnelData) {
		t.Errorf("Round-trip data mismatch:\n  got:  %x\n  want: %x", clean, tunnelData)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	tunnelData := []byte("response tunnel data with \x00\xFE bytes")
	padded := AddPadding(tunnelData)

	// Encode as Response
	respEnv := EncodeResponseEnvelope(padded, 7)
	senv, err := DecodeServerEnvelope(respEnv)
	if err != nil {
		t.Fatalf("DecodeServerEnvelope error: %v", err)
	}
	if senv.Type != "response" {
		t.Fatalf("Expected 'response', got '%s'", senv.Type)
	}

	payload, err := ExtractPayload(senv)
	if err != nil {
		t.Fatalf("ExtractPayload error: %v", err)
	}
	clean := StripPadding(payload)
	if !bytes.Equal(clean, tunnelData) {
		t.Errorf("Response round-trip mismatch:\n  got:  %x\n  want: %x", clean, tunnelData)
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	tunnelData := []byte("update tunnel data")
	padded := AddPadding(tunnelData)

	updEnv := EncodeUpdateEnvelope(padded)
	senv, err := DecodeServerEnvelope(updEnv)
	if err != nil {
		t.Fatalf("DecodeServerEnvelope error: %v", err)
	}
	if senv.Type != "update" {
		t.Fatalf("Expected 'update', got '%s'", senv.Type)
	}

	payload, err := ExtractPayload(senv)
	if err != nil {
		t.Fatalf("ExtractPayload error: %v", err)
	}
	clean := StripPadding(payload)
	if !bytes.Equal(clean, tunnelData) {
		t.Errorf("Update round-trip mismatch")
	}
}

func TestPaddingScheme(t *testing.T) {
	// Test with various data sizes
	testSizes := []int{0, 1, 10, 49, 50, 100, 499, 500, 1000, 4095, 4096, 10000, 65535}

	for _, size := range testSizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		padded := AddPadding(data)

		// Verify length prefix
		if len(padded) < 2 {
			t.Errorf("size %d: padded too short (%d)", size, len(padded))
			continue
		}
		storedLen := int(binary.BigEndian.Uint16(padded[0:2]))
		if storedLen != size {
			t.Errorf("size %d: stored length = %d", size, storedLen)
		}

		// Strip and verify
		clean := StripPadding(padded)
		if !bytes.Equal(clean, data) {
			t.Errorf("size %d: padding round-trip failed", size)
		}

		// Verify padded is larger or equal
		if len(padded) < size+2 {
			t.Errorf("size %d: padded (%d) smaller than data+2 (%d)", size, len(padded), size+2)
		}
	}
}

func TestPaddingWithFEBytes(t *testing.T) {
	// Critical: data containing 0xFE bytes must survive round-trip.
	// This is why we use length-prefix instead of 0xFE marker.
	data := make([]byte, 100)
	for i := range data {
		data[i] = 0xFE // all 0xFE
	}

	padded := AddPadding(data)
	clean := StripPadding(padded)

	if !bytes.Equal(clean, data) {
		t.Error("Data containing 0xFE bytes corrupted by padding round-trip")
	}
}

func TestStripPaddingInvalidInput(t *testing.T) {
	// Too short
	result := StripPadding([]byte{})
	if len(result) != 0 {
		t.Error("Expected empty for empty input")
	}

	result = StripPadding([]byte{0x01})
	if len(result) != 1 {
		t.Error("Expected passthrough for 1-byte input")
	}

	// Length exceeds buffer — should return as-is
	bad := []byte{0xFF, 0xFF, 0x01} // claims 65535 bytes, only has 1
	result = StripPadding(bad)
	if !bytes.Equal(result, bad) {
		t.Error("Expected passthrough for invalid length prefix")
	}
}

func TestFullClientToServerPipeline(t *testing.T) {
	// Simulate the complete flow: client wrap → Worker unwrap → backend
	originalData := make([]byte, 1500) // typical MTU-sized payload
	for i := range originalData {
		originalData[i] = byte(i % 256)
	}

	// Client wraps
	wrapped := WrapTunnelData(originalData, 5)

	// Worker decodes ClientEnvelope
	cenv, err := DecodeClientEnvelope(wrapped)
	if err != nil {
		t.Fatalf("Worker: DecodeClientEnvelope: %v", err)
	}
	if cenv.Type != "request" {
		t.Fatalf("Worker: expected request, got %s", cenv.Type)
	}

	// Worker decodes Request, strips padding
	req, err := DecodeRequest(cenv.Request)
	if err != nil {
		t.Fatalf("Worker: DecodeRequest: %v", err)
	}
	clean := StripPadding(req.Payload)

	if !bytes.Equal(clean, originalData) {
		t.Error("Full pipeline: data corrupted")
	}

	// Worker wraps response for client
	responseData := []byte("HTTP/1.1 200 OK\r\n\r\nHello")
	padded := AddPadding(responseData)
	respEnv := EncodeResponseEnvelope(padded, req.Index)

	// Client decodes ServerEnvelope
	senv, err := DecodeServerEnvelope(respEnv)
	if err != nil {
		t.Fatalf("Client: DecodeServerEnvelope: %v", err)
	}
	payload, err := ExtractPayload(senv)
	if err != nil {
		t.Fatalf("Client: ExtractPayload: %v", err)
	}
	responseClean := StripPadding(payload)
	if !bytes.Equal(responseClean, responseData) {
		t.Error("Full pipeline: response data corrupted")
	}
}

func BenchmarkWrapTunnelData(b *testing.B) {
	data := make([]byte, 1400)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		WrapTunnelData(data, i)
	}
}

func BenchmarkStripPadding(b *testing.B) {
	data := make([]byte, 1400)
	padded := AddPadding(data)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StripPadding(padded)
	}
}
