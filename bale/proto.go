package bale

import (
	"crypto/rand"
	"encoding/binary"
	mathrand "math/rand"
)

// Services is the set of real Bale gRPC service names used in request camouflage.
// The same names appear in legitimate Bale web client traffic.
var Services = []string{
	"bale.v1.Configs",
	"bale.users.v1.Users",
	"bale.auth.v1.Auth",
	"bale.fanoos.v1.fanoos",
	"bale.feedback.v1.FeedBack",
	"bale.ramz.v1.Ramz",
	"bale.report.v1.Report",
	"ai.bale.pushak.Push",
}

// Methods is the set of real Bale gRPC method names used in request camouflage.
var Methods = []string{
	"GetParameters",
	"GetContacts",
	"LoadUsers",
	"SearchContacts",
	"ImportContacts",
	"Send",
}

// MaxPayloadSize is the hard cap on a single wrapped tunnel payload (4 MiB).
// Payloads larger than this must be chunked by the caller. The length prefix
// is a 4-byte big-endian uint32, giving theoretical headroom to 4 GiB, but
// we cap well below that to keep framing efficient and DoS-resistant.
const MaxPayloadSize = 4 * 1024 * 1024

// ----------------------------------------------------------------------------
// PROTOBUF WRITER
// ----------------------------------------------------------------------------

// PbWriter is a minimal protobuf encoder matching the subset of the wire format
// that Bale's production client uses.
type PbWriter struct {
	buf []byte
}

// NewPbWriter returns an empty writer with a small starting capacity.
func NewPbWriter() *PbWriter {
	return &PbWriter{buf: make([]byte, 0, 256)}
}

// WriteVarint appends a protobuf varint.
func (w *PbWriter) WriteVarint(v uint32) *PbWriter {
	for v > 0x7F {
		w.buf = append(w.buf, byte(v&0x7F)|0x80)
		v >>= 7
	}
	w.buf = append(w.buf, byte(v&0x7F))
	return w
}

// WriteTag appends a protobuf tag (field number + wire type).
func (w *PbWriter) WriteTag(fieldNumber int, wireType int) *PbWriter {
	return w.WriteVarint(uint32((fieldNumber << 3) | wireType))
}

// WriteBytes appends a length-delimited byte field (wire type 2).
func (w *PbWriter) WriteBytes(fieldNumber int, data []byte) *PbWriter {
	w.WriteTag(fieldNumber, 2)
	w.WriteVarint(uint32(len(data)))
	w.buf = append(w.buf, data...)
	return w
}

// WriteString appends a length-delimited string field (wire type 2).
func (w *PbWriter) WriteString(fieldNumber int, s string) *PbWriter {
	return w.WriteBytes(fieldNumber, []byte(s))
}

// WriteInt32 appends a varint field (wire type 0). Skipped when v == 0
// to match protobuf's default-value omission behaviour.
func (w *PbWriter) WriteInt32(fieldNumber int, v int) *PbWriter {
	if v != 0 {
		w.WriteTag(fieldNumber, 0)
		w.WriteVarint(uint32(v))
	}
	return w
}

// Finish returns the encoded byte slice. The returned slice shares backing
// memory with the writer and must not be modified by the caller.
func (w *PbWriter) Finish() []byte {
	return w.buf
}

// ----------------------------------------------------------------------------
// PROTOBUF READER
// ----------------------------------------------------------------------------

// PbReader is a minimal protobuf decoder that tracks a position within a
// fixed input buffer.
type PbReader struct {
	buf []byte
	pos int
}

// NewPbReader returns a reader positioned at the start of data.
func NewPbReader(data []byte) *PbReader {
	return &PbReader{buf: data, pos: 0}
}

// Tag is the decoded representation of a protobuf field tag.
type Tag struct {
	FieldNumber int
	WireType    int
}

// ReadVarint reads and returns a protobuf varint.
func (r *PbReader) ReadVarint() (uint32, error) {
	var result uint32
	var shift uint
	for r.pos < len(r.buf) {
		b := r.buf[r.pos]
		r.pos++
		result |= uint32(b&0x7F) << shift
		if b&0x80 == 0 {
			return result, nil
		}
		shift += 7
		if shift > 35 {
			return 0, ErrVarintTooLong
		}
	}
	return 0, ErrUnexpectedEnd
}

// ReadTag reads a protobuf field tag.
func (r *PbReader) ReadTag() (Tag, error) {
	v, err := r.ReadVarint()
	if err != nil {
		return Tag{}, err
	}
	return Tag{
		FieldNumber: int(v >> 3),
		WireType:    int(v & 0x07),
	}, nil
}

// ReadBytes reads a length-delimited byte slice (wire type 2). The returned
// slice is a copy, so the caller is free to retain it.
func (r *PbReader) ReadBytes() ([]byte, error) {
	length, err := r.ReadVarint()
	if err != nil {
		return nil, err
	}
	if r.pos+int(length) > len(r.buf) {
		return nil, ErrUnexpectedEnd
	}
	data := make([]byte, length)
	copy(data, r.buf[r.pos:r.pos+int(length)])
	r.pos += int(length)
	return data, nil
}

// Skip advances past a single field given its wire type.
func (r *PbReader) Skip(wireType int) error {
	switch wireType {
	case 0:
		_, err := r.ReadVarint()
		return err
	case 1:
		if r.pos+8 > len(r.buf) {
			return ErrUnexpectedEnd
		}
		r.pos += 8
		return nil
	case 2:
		length, err := r.ReadVarint()
		if err != nil {
			return err
		}
		if r.pos+int(length) > len(r.buf) {
			return ErrUnexpectedEnd
		}
		r.pos += int(length)
		return nil
	case 5:
		if r.pos+4 > len(r.buf) {
			return ErrUnexpectedEnd
		}
		r.pos += 4
		return nil
	}
	return ErrUnknownWireType
}

// HasMore reports whether any bytes remain.
func (r *PbReader) HasMore() bool {
	return r.pos < len(r.buf)
}

// ----------------------------------------------------------------------------
// BALE MESSAGE ENCODING
// ----------------------------------------------------------------------------

// EncodeHandshakeRequest builds a ClientEnvelope { HandshakeRequest { mkprotoVersion=1, apiVersion=1 } }.
func EncodeHandshakeRequest() []byte {
	hs := NewPbWriter()
	hs.WriteInt32(1, 1)
	hs.WriteInt32(2, 1)
	env := NewPbWriter()
	env.WriteBytes(3, hs.Finish())
	return env.Finish()
}

// EncodePing builds a ClientEnvelope { Ping { id } }.
func EncodePing(id int) []byte {
	ping := NewPbWriter()
	if id != 0 {
		ping.WriteTag(1, 0)
		ping.WriteVarint(uint32(id))
	}
	env := NewPbWriter()
	env.WriteBytes(2, ping.Finish())
	return env.Finish()
}

// WrapTunnelData wraps raw tunnel bytes inside a Bale Request envelope that
// looks like a legitimate gRPC call (service name + method + payload + index).
// Oversize inputs (> MaxPayloadSize) are truncated; the caller is responsible
// for chunking large payloads before calling this function.
func WrapTunnelData(tunnelBytes []byte, requestIndex int) []byte {
	if len(tunnelBytes) > MaxPayloadSize {
		tunnelBytes = tunnelBytes[:MaxPayloadSize]
	}
	service := Services[requestIndex%len(Services)]
	method := Methods[requestIndex%len(Methods)]
	padded := AddPadding(tunnelBytes)
	req := NewPbWriter()
	req.WriteString(1, service)
	req.WriteString(2, method)
	req.WriteBytes(3, padded)
	req.WriteTag(5, 0)
	req.WriteVarint(uint32(requestIndex))
	env := NewPbWriter()
	env.WriteBytes(1, req.Finish())
	return env.Finish()
}

// ----------------------------------------------------------------------------
// SERVER ENVELOPE DECODING
// ----------------------------------------------------------------------------

// ServerEnvelope is a decoded server-to-client message.
type ServerEnvelope struct {
	Type              string
	Response          []byte
	Update            []byte
	Pong              []byte
	HandshakeResponse []byte
}

// DecodeServerEnvelope parses a raw ServerEnvelope.
func DecodeServerEnvelope(data []byte) (*ServerEnvelope, error) {
	r := NewPbReader(data)
	env := &ServerEnvelope{Type: "unknown"}
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		switch tag.FieldNumber {
		case 1:
			env.Response, err = r.ReadBytes()
			env.Type = "response"
		case 2:
			env.Update, err = r.ReadBytes()
			env.Type = "update"
		case 3:
			env.Type = "terminate"
			err = r.Skip(tag.WireType)
		case 4:
			env.Pong, err = r.ReadBytes()
			env.Type = "pong"
		case 5:
			env.HandshakeResponse, err = r.ReadBytes()
			env.Type = "handshake"
		default:
			err = r.Skip(tag.WireType)
		}
		if err != nil {
			return nil, err
		}
	}
	return env, nil
}

// ExtractPayload returns the tunnel payload inside a Response or Update envelope.
func ExtractPayload(env *ServerEnvelope) ([]byte, error) {
	switch env.Type {
	case "response":
		if env.Response == nil {
			return nil, nil
		}
		return extractFromResponse(env.Response)
	case "update":
		if env.Update == nil {
			return nil, nil
		}
		return extractFromUpdate(env.Update)
	}
	return nil, nil
}

// extractFromResponse pulls the payload bytes out of a Response message.
// Bale places the tunnel bytes at field number 2; field number 3 is an
// integer index we deliberately ignore on the client side.
func extractFromResponse(data []byte) ([]byte, error) {
	r := NewPbReader(data)
	var payload []byte
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		switch tag.FieldNumber {
		case 2:
			payload, err = r.ReadBytes()
		case 3:
			_, err = r.ReadVarint()
		default:
			err = r.Skip(tag.WireType)
		}
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}

// extractFromUpdate pulls the payload bytes out of an Update message.
func extractFromUpdate(data []byte) ([]byte, error) {
	r := NewPbReader(data)
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		if tag.FieldNumber == 1 {
			return r.ReadBytes()
		}
		if err := r.Skip(tag.WireType); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

// ----------------------------------------------------------------------------
// PADDING — 4-byte length-prefix scheme
// ----------------------------------------------------------------------------
//
// Encoded layout:  [uint32_be real_length] [real_data...] [random_padding...]
//
// The uint32 prefix supports payloads up to 4 GiB (the caller-enforced cap is
// MaxPayloadSize = 4 MiB). The random padding bytes come from crypto/rand for
// traffic-analysis resistance; the target-size jitter can continue to use
// math/rand since it is only a length-distribution obfuscator, not a secret.

const paddingLenPrefix = 4

// AddPadding prepends a 4-byte big-endian length header and appends random
// padding to obfuscate the true payload length distribution.
func AddPadding(data []byte) []byte {
	dataLen := len(data)
	var targetSize int
	switch {
	case dataLen < 50:
		targetSize = 50 + mathrand.Intn(150)
	case dataLen < 500:
		targetSize = maxInt(dataLen, 200) + mathrand.Intn(300)
	case dataLen < 4096:
		targetSize = dataLen + mathrand.Intn(512)
	default:
		targetSize = dataLen + mathrand.Intn(64)
	}
	if targetSize < dataLen {
		targetSize = dataLen
	}
	paddingLen := targetSize - dataLen
	result := make([]byte, paddingLenPrefix+dataLen+paddingLen)
	binary.BigEndian.PutUint32(result[0:paddingLenPrefix], uint32(dataLen))
	copy(result[paddingLenPrefix:], data)
	if paddingLen > 0 {
		// crypto/rand for the padding bytes themselves — resists
		// statistical analysis on padding content.
		if _, err := rand.Read(result[paddingLenPrefix+dataLen:]); err != nil {
			// Fallback to math/rand on the (theoretical) crypto/rand
			// failure — obfuscation padding is not cryptographically
			// critical, and leaving the bytes zeroed would leak
			// information. This path is effectively unreachable on
			// supported platforms.
			for i := paddingLenPrefix + dataLen; i < len(result); i++ {
				result[i] = byte(mathrand.Intn(256))
			}
		}
	}
	return result
}

// StripPadding reverses AddPadding. On malformed input (too short, or a stated
// length that exceeds the buffer) it returns the input unchanged so that the
// caller can observe the anomaly rather than crashing the reader loop.
func StripPadding(data []byte) []byte {
	if len(data) < paddingLenPrefix {
		return data
	}
	realLen := int(binary.BigEndian.Uint32(data[0:paddingLenPrefix]))
	if realLen < 0 || realLen+paddingLenPrefix > len(data) {
		return data
	}
	return data[paddingLenPrefix : paddingLenPrefix+realLen]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ----------------------------------------------------------------------------
// SERVER ENVELOPE ENCODING (used by unwrapper / Worker)
// ----------------------------------------------------------------------------

// EncodeHandshakeResponse builds a ServerEnvelope { HandshakeResponse { ... } }.
func EncodeHandshakeResponse() []byte {
	hs := NewPbWriter()
	hs.WriteInt32(1, 1) // mkprotoVersion
	hs.WriteInt32(2, 1) // apiVersion
	env := NewPbWriter()
	env.WriteBytes(5, hs.Finish()) // field 5 = handshakeResponse
	return env.Finish()
}

// EncodePong builds a ServerEnvelope { Pong { id } }.
func EncodePong(id int) []byte {
	pong := NewPbWriter()
	if id != 0 {
		pong.WriteTag(1, 0)
		pong.WriteVarint(uint32(id))
	}
	env := NewPbWriter()
	env.WriteBytes(4, pong.Finish()) // field 4 = pong
	return env.Finish()
}

// EncodeResponseEnvelope builds a ServerEnvelope { Response { response=data, index=idx } }.
func EncodeResponseEnvelope(data []byte, index int) []byte {
	resp := NewPbWriter()
	if len(data) > 0 {
		resp.WriteBytes(2, data)
	}
	if index != 0 {
		resp.WriteTag(3, 0)
		resp.WriteVarint(uint32(index))
	}
	env := NewPbWriter()
	env.WriteBytes(1, resp.Finish())
	return env.Finish()
}

// EncodeUpdateEnvelope builds a ServerEnvelope { Update { update=data } }.
func EncodeUpdateEnvelope(data []byte) []byte {
	upd := NewPbWriter()
	if len(data) > 0 {
		upd.WriteBytes(1, data)
	}
	env := NewPbWriter()
	env.WriteBytes(2, upd.Finish())
	return env.Finish()
}

// ----------------------------------------------------------------------------
// CLIENT ENVELOPE DECODING (used by unwrapper / Worker)
// ----------------------------------------------------------------------------

// ClientEnvelope is a decoded client-to-server message.
type ClientEnvelope struct {
	Type             string // "request", "ping", "handshake", "unknown"
	Request          []byte
	Ping             []byte
	HandshakeRequest []byte
}

// DecodeClientEnvelope parses a raw ClientEnvelope.
func DecodeClientEnvelope(data []byte) (*ClientEnvelope, error) {
	r := NewPbReader(data)
	env := &ClientEnvelope{Type: "unknown"}
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		switch tag.FieldNumber {
		case 1:
			env.Request, err = r.ReadBytes()
			env.Type = "request"
		case 2:
			env.Ping, err = r.ReadBytes()
			env.Type = "ping"
		case 3:
			env.HandshakeRequest, err = r.ReadBytes()
			env.Type = "handshake"
		default:
			err = r.Skip(tag.WireType)
		}
		if err != nil {
			return nil, err
		}
	}
	return env, nil
}

// DecodedRequest holds the parsed fields from a Request message.
type DecodedRequest struct {
	ServiceName string
	Method      string
	Payload     []byte
	Index       int
}

// DecodeRequest parses a Request message to extract the tunnel payload.
func DecodeRequest(data []byte) (*DecodedRequest, error) {
	r := NewPbReader(data)
	req := &DecodedRequest{}
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		switch tag.FieldNumber {
		case 1:
			var s []byte
			s, err = r.ReadBytes()
			req.ServiceName = string(s)
		case 2:
			var s []byte
			s, err = r.ReadBytes()
			req.Method = string(s)
		case 3:
			req.Payload, err = r.ReadBytes()
		case 5:
			v, e := r.ReadVarint()
			req.Index = int(v)
			err = e
		default:
			err = r.Skip(tag.WireType)
		}
		if err != nil {
			return nil, err
		}
	}
	return req, nil
}

// DecodePing extracts the ping ID from a Ping message.
func DecodePing(data []byte) (int, error) {
	r := NewPbReader(data)
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return 0, err
		}
		if tag.FieldNumber == 1 {
			v, err := r.ReadVarint()
			return int(v), err
		}
		if err := r.Skip(tag.WireType); err != nil {
			return 0, err
		}
	}
	return 0, nil
}
