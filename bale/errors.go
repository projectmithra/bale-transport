package bale

import "errors"

var (
	ErrVarintTooLong   = errors.New("bale: varint too long")
	ErrUnexpectedEnd   = errors.New("bale: unexpected end of buffer")
	ErrUnknownWireType = errors.New("bale: unknown wire type")
)
