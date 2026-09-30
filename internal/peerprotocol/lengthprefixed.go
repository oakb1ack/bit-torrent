package peerprotocol

import (
	"fmt"
	"encoding/binary"
)

type LengthPrefixed struct {
	Length    [4]byte
	MessageID byte
	Payload   []byte
}

func EncodeLengthPrefixed(lengthPrefixed LengthPrefixed) ([]byte, error){
	var ans []byte
	var n uint32 = 0

	for _, value := range lengthPrefixed.Length {
		ans = append(ans, value) 
		n = (n << 8 ) | uint32(value) 
	}

	ans = append(ans, lengthPrefixed.MessageID) 

	if n != uint32(len(lengthPrefixed.Payload)) + 1 {
		return nil, fmt.Errorf("Payload length does not match message size")
	}

	for _, value := range lengthPrefixed.Payload {
		ans = append(ans, value)
	}

	return ans, nil
}

func DecodeLengthPrefixed(ans []byte) (LengthPrefixed, error) {
	var lengthPrefixed LengthPrefixed

	if ans == nil {
		return lengthPrefixed, fmt.Errorf("No byte stream detected for decoding")
	}
	if len(ans) < 5 {
		return lengthPrefixed, fmt.Errorf("Input byte stream too short")
	}

	copy(lengthPrefixed.Length[:], ans[0:4])

	n := binary.BigEndian.Uint32(ans[:4])

	lengthPrefixed.MessageID = ans[4]
	if n != uint32(0) {
		copy(lengthPrefixed.Payload[:], ans[5:])
	}

	return lengthPrefixed, nil
}
