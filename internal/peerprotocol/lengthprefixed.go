package peerprotocol

import (
	"encoding/binary"
	"fmt"
)

type LengthPrefixed struct {
	Length    [4]byte
	MessageID byte
	Payload   []byte
}

func EncodeLengthPrefixed(lengthPrefixed LengthPrefixed) ([]byte, error) {
	var ans []byte
	var n uint32 = 0

	for _, value := range lengthPrefixed.Length {
		ans = append(ans, value)
		n = (n << 8) | uint32(value)
	}

	ans = append(ans, lengthPrefixed.MessageID)

	if n != uint32(len(lengthPrefixed.Payload))+1 {
		return nil, fmt.Errorf("Payload length does not match message size")
	}

	for _, value := range lengthPrefixed.Payload {
		ans = append(ans, value)
	}

	return ans, nil
}

func DecodeLengthPrefixed(ans []byte) (LengthPrefixed, error) {
	var lengthPrefixed LengthPrefixed

	if len(ans) < 4 {
		return lengthPrefixed, fmt.Errorf("input is too short to contain a length prefix")
	}

	copy(lengthPrefixed.Length[:], ans[:4])
	n := binary.BigEndian.Uint32(lengthPrefixed.Length[:])
	if n == 0 {
		return lengthPrefixed, fmt.Errorf("zero-length frame is a keep-alive message")
	}
	if uint64(n) != uint64(len(ans)-4) {
		return lengthPrefixed, fmt.Errorf("length prefix does not match frame size")
	}

	lengthPrefixed.MessageID = ans[4]
	lengthPrefixed.Payload = append([]byte(nil), ans[5:]...)

	return lengthPrefixed, nil
}
