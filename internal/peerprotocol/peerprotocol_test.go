package peerprotocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHandshakeEncodeDecode(t *testing.T) {
	var infoHash [20]byte
	var peerID [20]byte
	for i := range infoHash {
		infoHash[i] = byte(i)
		peerID[i] = byte(20 + i)
	}

	handshake := Handshake{
		ProtocolStringLength: byte(len(bitTorrentProtocol)),
		ProtocolString:       bitTorrentProtocol,
		InfoHash:             infoHash,
		PeerID:               peerID,
	}

	encoded, err := EncodeHandshake(handshake)
	if err != nil {
		t.Fatalf("EncodeHandshake() error = %v", err)
	}
	if want := 49 + len(bitTorrentProtocol); len(encoded) != want {
		t.Fatalf("encoded handshake length = %d, want %d", len(encoded), want)
	}
	if encoded[0] != byte(len(bitTorrentProtocol)) {
		t.Fatalf("protocol length byte = %d, want %d", encoded[0], len(bitTorrentProtocol))
	}
	if !bytes.Equal(encoded[1:1+len(bitTorrentProtocol)], []byte(bitTorrentProtocol)) {
		t.Fatalf("encoded protocol string = %q, want %q", encoded[1:1+len(bitTorrentProtocol)], bitTorrentProtocol)
	}

	decoded, err := DecodeHandshake(encoded)
	if err != nil {
		t.Fatalf("DecodeHandshake() error = %v", err)
	}
	if decoded != handshake {
		t.Fatalf("decoded handshake = %#v, want %#v", decoded, handshake)
	}
}

func TestEncodeHandshakeRejectsInvalidProtocol(t *testing.T) {
	handshake := Handshake{
		ProtocolStringLength: byte(len(bitTorrentProtocol)),
		ProtocolString:       "not BitTorrent",
	}
	if _, err := EncodeHandshake(handshake); err == nil {
		t.Fatal("EncodeHandshake() error = nil, want unsupported protocol error")
	}
}

func TestEncodeHandshakeRejectsMismatchedProtocolLength(t *testing.T) {
	handshake := Handshake{
		ProtocolStringLength: byte(len(bitTorrentProtocol) - 1),
		ProtocolString:       bitTorrentProtocol,
	}
	if _, err := EncodeHandshake(handshake); err == nil {
		t.Fatal("EncodeHandshake() error = nil, want protocol length error")
	}
}

func TestDecodeHandshakeRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "empty"},
		{name: "truncated", input: []byte{byte(len(bitTorrentProtocol))}},
		{name: "unsupported protocol", input: func() []byte {
			data := make([]byte, 49+len(bitTorrentProtocol))
			data[0] = byte(len(bitTorrentProtocol))
			copy(data[1:], "Other protocol name")
			return data
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeHandshake(tt.input); err == nil {
				t.Fatal("DecodeHandshake() error = nil, want malformed input error")
			}
		})
	}
}

func TestKeepAliveEncodeDecode(t *testing.T) {
	encoded := EncodeKeepAlive()
	if want := []byte{0, 0, 0, 0}; !bytes.Equal(encoded, want) {
		t.Fatalf("EncodeKeepAlive() = %v, want %v", encoded, want)
	}
	if err := DecodeKeepAlive(encoded); err != nil {
		t.Fatalf("DecodeKeepAlive() error = %v", err)
	}
}

func TestDecodeKeepAliveRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "short", input: []byte{0, 0, 0}},
		{name: "nonzero length", input: []byte{0, 0, 0, 1}},
		{name: "extra bytes", input: []byte{0, 0, 0, 0, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := DecodeKeepAlive(tt.input); err == nil {
				t.Fatal("DecodeKeepAlive() error = nil, want malformed input error")
			}
		})
	}
}

func TestLengthPrefixedEncodeDecode(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30}
	frame := LengthPrefixed{
		Length:    [4]byte{0, 0, 0, 4},
		MessageID: 6,
		Payload:   payload,
	}

	encoded, err := EncodeLengthPrefixed(frame)
	if err != nil {
		t.Fatalf("EncodeLengthPrefixed() error = %v", err)
	}
	if want := []byte{0, 0, 0, 4, 6, 0x10, 0x20, 0x30}; !bytes.Equal(encoded, want) {
		t.Fatalf("encoded frame = %v, want %v", encoded, want)
	}

	decoded, err := DecodeLengthPrefixed(encoded)
	if err != nil {
		t.Fatalf("DecodeLengthPrefixed() error = %v", err)
	}
	if decoded.Length != frame.Length || decoded.MessageID != frame.MessageID || !bytes.Equal(decoded.Payload, payload) {
		t.Fatalf("decoded frame = %#v, want %#v", decoded, frame)
	}
}

func TestEncodeLengthPrefixedRejectsMismatchedLength(t *testing.T) {
	frame := LengthPrefixed{
		Length:    [4]byte{0, 0, 0, 1},
		MessageID: 4,
		Payload:   []byte{0x01},
	}
	if _, err := EncodeLengthPrefixed(frame); err == nil {
		t.Fatal("EncodeLengthPrefixed() error = nil, want length mismatch error")
	}
}

func TestDecodeLengthPrefixedRejectsMalformedInput(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
	}{
		{name: "short prefix", input: []byte{0, 0, 0}},
		{name: "keep-alive", input: []byte{0, 0, 0, 0}},
		{name: "missing message ID", input: []byte{0, 0, 0, 1}},
		{name: "truncated payload", input: []byte{0, 0, 0, 2, 4}},
		{name: "trailing byte", input: []byte{0, 0, 0, 1, 4, 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DecodeLengthPrefixed(tt.input); err == nil {
				t.Fatal("DecodeLengthPrefixed() error = nil, want malformed input error")
			}
		})
	}
}

func TestLengthPrefixedUsesNetworkByteOrder(t *testing.T) {
	input := make([]byte, 5)
	binary.BigEndian.PutUint32(input[:4], 1)
	input[4] = 3

	decoded, err := DecodeLengthPrefixed(input)
	if err != nil {
		t.Fatalf("DecodeLengthPrefixed() error = %v", err)
	}
	if decoded.Length != [4]byte{0, 0, 0, 1} {
		t.Fatalf("decoded length = %v, want [0 0 0 1]", decoded.Length)
	}
	if decoded.MessageID != 3 {
		t.Fatalf("decoded message ID = %d, want 3", decoded.MessageID)
	}
}
