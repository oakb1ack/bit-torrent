package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestEmptyMessages(t *testing.T) {
	tests := []struct {
		name   string
		id     byte
		encode func() ([]byte, error)
		decode func([]byte) error
	}{
		{"choke", 0, EncodeChoke, DecodeChoke},
		{"unchoke", 1, EncodeUnchoke, DecodeUnchoke},
		{"interested", 2, EncodeInterested, DecodeInterested},
		{"uninterested", 3, EncodeUninterested, DecodeUninterested},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := []byte{0, 0, 0, 1, tt.id}
			encoded, err := tt.encode()
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatalf("encode = %x, %v; want %x, nil", encoded, err, wire)
			}
			if err := tt.decode(wire); err != nil {
				t.Fatalf("decode: %v", err)
			}
			for _, malformed := range [][]byte{
				nil,
				{0, 0, 0, 1, (tt.id + 1) % 4},
				{0, 0, 0, 2, tt.id, 0},
			} {
				if err := tt.decode(malformed); err == nil {
					t.Errorf("accepted malformed message %x", malformed)
				}
			}
		})
	}
}

func TestHaveWireFormat(t *testing.T) {
	const index uint32 = 0x01020304
	wire := []byte{0, 0, 0, 5, 4, 1, 2, 3, 4}
	encoded, err := EncodeHave(index)
	if err != nil || !bytes.Equal(encoded, wire) {
		t.Fatalf("encode = %x, %v; want %x, nil", encoded, err, wire)
	}
	decoded, err := DecodeHave(wire)
	if err != nil || decoded != index {
		t.Fatalf("decode = %d, %v; want %d, nil", decoded, err, index)
	}
}

func TestBlockRequestWireFormat(t *testing.T) {
	tests := []struct {
		name   string
		id     byte
		encode func(BlockRequest) ([]byte, error)
		decode func([]byte) (BlockRequest, error)
	}{
		{"request", 6, EncodeRequest, DecodeRequest},
		{"cancel", 8, EncodeCancel, DecodeCancel},
	}
	request := BlockRequest{Index: 0x01020304, Begin: 0x00010000, Length: 16384}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := []byte{0, 0, 0, 13, tt.id, 1, 2, 3, 4, 0, 1, 0, 0, 0, 0, 0x40, 0}
			encoded, err := tt.encode(request)
			if err != nil || !bytes.Equal(encoded, wire) {
				t.Fatalf("encode = %x, %v; want %x, nil", encoded, err, wire)
			}
			decoded, err := tt.decode(wire)
			if err != nil || decoded != request {
				t.Fatalf("decode = %#v, %v; want %#v, nil", decoded, err, request)
			}
		})
	}
}

func TestPieceWireFormat(t *testing.T) {
	piece := Piece{Index: 0x01020304, Begin: 0x00010000, Block: []byte{0, 0xff, 0x80}}
	wire := []byte{0, 0, 0, 12, 7, 1, 2, 3, 4, 0, 1, 0, 0, 0, 0xff, 0x80}
	encoded, err := EncodePiece(piece)
	if err != nil || !bytes.Equal(encoded, wire) {
		t.Fatalf("encode = %x, %v; want %x, nil", encoded, err, wire)
	}
	decoded, err := DecodePiece(wire)
	if err != nil || decoded.Index != piece.Index || decoded.Begin != piece.Begin || !bytes.Equal(decoded.Block, piece.Block) {
		t.Fatalf("decode = %#v, %v; want %#v, nil", decoded, err, piece)
	}
	// Decoding must not retain the caller's receive buffer.
	wire[13] = 0x55
	if !bytes.Equal(decoded.Block, piece.Block) {
		t.Fatal("decoded block changed when the wire buffer was reused")
	}
	piece.Block[0] = 0x66
	if encoded[13] != 0 {
		t.Fatal("encoded block aliases the input block")
	}
}

func TestVariablePayloadLengths(t *testing.T) {
	// Cross the one-byte length boundary to verify the complete big-endian prefix.
	block := bytes.Repeat([]byte{0xa5}, 300)
	encoded, err := EncodePiece(Piece{Index: 2, Begin: 16384, Block: block})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded[:4], []byte{0, 0, 1, 0x35}) {
		t.Fatalf("piece length prefix = %x; want 00000135", encoded[:4])
	}
	piece, err := DecodePiece(encoded)
	if err != nil || !bytes.Equal(piece.Block, block) {
		t.Fatalf("piece decode error = %v; block length = %d", err, len(piece.Block))
	}
	bitfield := bytes.Repeat([]byte{0xff}, 256)
	encoded, err = EncodeBitfield(bitfield, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded[:4], []byte{0, 0, 1, 1}) {
		t.Fatalf("bitfield length prefix = %x; want 00000101", encoded[:4])
	}
	decoded, err := DecodeBitfield(encoded, 2048)
	if err != nil || !bytes.Equal(decoded, bitfield) {
		t.Fatalf("bitfield decode error = %v; payload length = %d", err, len(decoded))
	}
}

func TestPayloadDecodersRejectMalformedFrames(t *testing.T) {
	tests := []struct {
		name    string
		id      byte
		payload []byte
		decode  func([]byte) error
	}{
		{"have", 4, make([]byte, 4), func(b []byte) error { _, err := DecodeHave(b); return err }},
		{"request", 6, make([]byte, 12), func(b []byte) error { _, err := DecodeRequest(b); return err }},
		{"cancel", 8, make([]byte, 12), func(b []byte) error { _, err := DecodeCancel(b); return err }},
		{"piece", 7, make([]byte, 8), func(b []byte) error { _, err := DecodePiece(b); return err }},
		{"bitfield", 5, []byte{0x80}, func(b []byte) error { _, err := DecodeBitfield(b, 1); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wire := payloadTestFrame(tt.id, tt.payload)
			wrongID := append([]byte(nil), wire...)
			wrongID[4] = 0
			malformed := [][]byte{
				nil,
				{0, 0, 0},
				{0, 0, 0, 0},
				{0, 0, 0, 1},
				wire[:len(wire)-1],
				append(append([]byte(nil), wire...), 0),
				wrongID,
				payloadTestFrame(tt.id, tt.payload[:len(tt.payload)-1]),
			}
			if tt.name != "piece" { // A piece's block has variable length.
				malformed = append(malformed, payloadTestFrame(tt.id, append(append([]byte(nil), tt.payload...), 0)))
			}
			for _, message := range malformed {
				if err := tt.decode(message); err == nil {
					t.Errorf("accepted malformed message %x", message)
				}
			}
		})
	}
}

func TestBitfieldWireFormat(t *testing.T) {
	tests := []struct {
		name       string
		pieceCount int
		bits       []byte
		wire       []byte
	}{
		{"zero pieces", 0, nil, []byte{0, 0, 0, 1, 5}},
		{"one piece", 1, []byte{0x80}, []byte{0, 0, 0, 2, 5, 0x80}},
		{"no available pieces", 9, []byte{0, 0}, []byte{0, 0, 0, 3, 5, 0, 0}},
		{"byte aligned", 8, []byte{0x81}, []byte{0, 0, 0, 2, 5, 0x81}},
		{"pieces zero seven eight", 9, []byte{0x81, 0x80}, []byte{0, 0, 0, 3, 5, 0x81, 0x80}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := EncodeBitfield(tt.bits, tt.pieceCount)
			if err != nil || !bytes.Equal(encoded, tt.wire) {
				t.Fatalf("encode = %x, %v; want %x, nil", encoded, err, tt.wire)
			}
			decoded, err := DecodeBitfield(tt.wire, tt.pieceCount)
			if err != nil || !bytes.Equal(decoded, tt.bits) {
				t.Fatalf("decode = %x, %v; want %x, nil", decoded, err, tt.bits)
			}
			if len(tt.bits) != 0 {
				tt.wire[5] ^= 0xff
				if !bytes.Equal(decoded, tt.bits) {
					t.Fatal("decoded bitfield aliases the wire buffer")
				}
				tt.bits[0] ^= 0xff
				if encoded[5] != decoded[0] {
					t.Fatal("encoded bitfield aliases the input bits")
				}
			}
		})
	}
}

func TestBitfieldRejectsInvalidBits(t *testing.T) {
	tests := []struct {
		name       string
		pieceCount int
		bits       []byte
	}{
		{"negative count", -1, nil},
		{"missing byte", 9, []byte{0x80}},
		{"extra byte", 8, []byte{0x80, 0}},
		{"unexpected bits for zero pieces", 0, []byte{0}},
		{"spare bit in first byte", 1, []byte{0x81}},
		{"spare bit in last byte", 9, []byte{0x81, 0x40}},
		{"max int piece count", int(^uint(0) >> 1), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := EncodeBitfield(tt.bits, tt.pieceCount); err == nil {
				t.Fatal("encode accepted invalid bitfield")
			}
			if _, err := DecodeBitfield(payloadTestFrame(5, tt.bits), tt.pieceCount); err == nil {
				t.Fatal("decode accepted invalid bitfield")
			}
		})
	}
	// Every partial-byte boundary must reject all unused low bits.
	for count := 1; count < 8; count++ {
		for bit := 0; bit < 8-count; bit++ {
			if _, err := EncodeBitfield([]byte{1 << uint(bit)}, count); err == nil {
				t.Errorf("count %d accepted spare bit %d", count, bit)
			}
		}
	}
}

func payloadTestFrame(id byte, payload []byte) []byte {
	frame := make([]byte, 5+len(payload))
	binary.BigEndian.PutUint32(frame[:4], uint32(1+len(payload)))
	frame[4] = id
	copy(frame[5:], payload)
	return frame
}
