package bale

import (
	"encoding/binary"
	"math/rand"
)

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

var Methods = []string{
	"GetParameters",
	"GetContacts",
	"LoadUsers",
	"SearchContacts",
	"ImportContacts",
	"Send",
}

// ============================================================
// PROTOBUF WRITER
// ============================================================

type PbWriter struct {
	buf []byte
}

func NewPbWriter() *PbWriter {
	return &PbWriter{buf: make([]byte, 0, 256)}
}

func (w *PbWriter) WriteVarint(v uint32) *PbWriter {
	for v > 0x7F {
		w.buf = append(w.buf, byte(v&0x7F)|0x80)
		v >>= 7
	}
	w.buf = append(w.buf, byte(v&0x7F))
	return w
}

func (w *PbWriter) WriteTag(fieldNumber int, wireType int) *PbWriter {
	return w.WriteVarint(uint32((fieldNumber << 3) | wireType))
}

func (w *PbWriter) WriteBytes(fieldNumber int, data []byte) *PbWriter {
	w.WriteTag(fieldNumber, 2)
	w.WriteVarint(uint32(len(data)))
	w.buf = append(w.buf, data...)
	return w
}

func (w *PbWriter) WriteString(fieldNumber int, s string) *PbWriter {
	return w.WriteBytes(fieldNumber, []byte(s))
}

func (w *PbWriter) WriteInt32(fieldNumber int, v int) *PbWriter {
	if v != 0 {
		w.WriteTag(fieldNumber, 0)
		w.WriteVarint(uint32(v))
	}
	return w
}

func (w *PbWriter) Finish() []byte {
	return w.buf
}

// ============================================================
// PROTOBUF READER
// ============================================================

type PbReader struct {
	buf []byte
	pos int
}

func NewPbReader(data []byte) *PbReader {
	return &PbReader{buf: data, pos: 0}
}

type Tag struct {
	FieldNumber int
	WireType    int
}

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

func (r *PbReader) HasMore() bool {
	return r.pos < len(r.buf)
}

// ============================================================
// BALE MESSAGE ENCODING
// ============================================================

func EncodeHandshakeRequest() []byte {
	hs := NewPbWriter()
	hs.WriteInt32(1, 1)
	hs.WriteInt32(2, 1)
	env := NewPbWriter()
	env.WriteBytes(3, hs.Finish())
	return env.Finish()
}

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

func WrapTunnelData(tunnelBytes []byte, requestIndex int) []byte {
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

// ============================================================
// SERVER ENVELOPE DECODING
// ============================================================

type ServerEnvelope struct {
	Type              string
	Response          []byte
	Update            []byte
	Pong              []byte
	HandshakeResponse []byte
}

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

func extractFromResponse(data []byte) ([]byte, error) {
	r := NewPbReader(data)
	var payload []byte
	for r.HasMore() {
		tag, err := r.ReadTag()
		if err != nil {
			return nil, err
		}
		switch tag.FieldNumber {
		case 1:
			payload, err = r.ReadBytes()
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

// ============================================================
// PADDING — Length-prefix scheme
// ============================================================

func AddPadding(data []byte) []byte {
	dataLen := len(data)
	var targetSize int
	switch {
	case dataLen < 50:
		targetSize = 50 + rand.Intn(150)
	case dataLen < 500:
		targetSize = maxInt(dataLen, 200) + rand.Intn(300)
	case dataLen < 4096:
		targetSize = dataLen + rand.Intn(512)
	default:
		targetSize = dataLen + rand.Intn(64)
	}
	if targetSize <= dataLen {
		result := make([]byte, 2+dataLen)
		binary.BigEndian.PutUint16(result[0:2], uint16(dataLen))
		copy(result[2:], data)
		return result
	}
	paddingLen := targetSize - dataLen
	result := make([]byte, 2+dataLen+paddingLen)
	binary.BigEndian.PutUint16(result[0:2], uint16(dataLen))
	copy(result[2:], data)
	for i := 2 + dataLen; i < len(result); i++ {
		result[i] = byte(rand.Intn(256))
	}
	return result
}

func StripPadding(data []byte) []byte {
	if len(data) < 2 {
		return data
	}
	realLen := int(binary.BigEndian.Uint16(data[0:2]))
	if realLen+2 > len(data) {
		return data
	}
	return data[2 : 2+realLen]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
