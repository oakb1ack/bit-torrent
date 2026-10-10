package peerprotocol

import "fmt"

const (
	chokeMessageID         byte = 0
	unchokeMessageID       byte = 1
	interestedMessageID    byte = 2
	uninterestedMessageID  byte = 3
	haveMessageID          byte = 4
	bitfieldMessageID      byte = 5
	requestMessageID       byte = 6
	pieceMessageID         byte = 7
	cancelMessageID        byte = 8
)

func EncodeChoke() ([]byte, error) {
	return encodeEmptyMessage(chokeMessageID)
}

func DecodeChoke(message []byte) error {
	return decodeEmptyMessage(message, chokeMessageID)
}

func EncodeUnchoke() ([]byte, error) {
	return encodeEmptyMessage(unchokeMessageID)
}

func DecodeUnchoke(message []byte) error {
	return decodeEmptyMessage(message, unchokeMessageID)
}

func EncodeInterested() ([]byte, error) {
	return encodeEmptyMessage(interestedMessageID)
}

func DecodeInterested(message []byte) error {
	return decodeEmptyMessage(message, uninterestedMessageID)
}

func EncodeUninterested() ([]byte, error) {
	return encodeEmptyMessage(interestedMessageID)
}

func DecodeUninterested(message []byte) error {
	return decodeEmptyMessage(message, uninterestedMessageID)
}

func EncodeHave() ([]byte, error) {
}

func DecodeHave(message []byte) error {
}

func encodeEmptyMessage(messageID byte) ([]byte, error) {
	return EncodeLengthPrefixed(LengthPrefixed{
		Length:    [4]byte{0, 0, 0, 1},
		MessageID: messageID,
	})
}

func decodeEmptyMessage(message []byte, expectedMessageID byte) error {
	frame, err := DecodeLengthPrefixed(message)
	if err != nil {
		return err
	}
	if frame.MessageID != expectedMessageID {
		return fmt.Errorf("unexpected message ID %d; want %d", frame.MessageID, expectedMessageID)
	}
	if len(frame.Payload) != 0 {
		return fmt.Errorf("message ID %d must not have a payload", expectedMessageID)
	}
	return nil
}

