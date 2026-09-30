package peerprotocol

import (
	"fmt"
)

func EncodeKeepAlive() []byte {
	return []byte{0x00, 0x00, 0x00, 0x00}
}

func DecodeKeepAlive(message []byte) error {
	if len(message) != 4 {
		return fmt.Errorf("message not length 4")
	}
	if message[0] != 0x00 || message[1] != 0x00 || message[2] != 0x00 || message[3] != 0x00 {
		return fmt.Errorf("message does not contain 4 0s")
	}

	return nil
}
