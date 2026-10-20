package peerprotocol

import (
	"encoding/binary"
	"fmt"
)

const (
	chokeMessageID        byte = 0
	unchokeMessageID      byte = 1
	interestedMessageID   byte = 2
	uninterestedMessageID byte = 3
	haveMessageID         byte = 4
	bitfieldMessageID     byte = 5
	requestMessageID      byte = 6
	pieceMessageID        byte = 7
	cancelMessageID       byte = 8
)

type BlockRequest struct {
	Index  uint32 // Piece index
	Begin  uint32 // Byte offset within that piece
	Length uint32 // Number of bytes requested
}

type Piece struct {
	Index uint32
	Begin uint32
	Block []byte
}

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
	return decodeEmptyMessage(message, interestedMessageID)
}

func EncodeUninterested() ([]byte, error) {
	return encodeEmptyMessage(uninterestedMessageID)
}

func DecodeUninterested(message []byte) error {
	return decodeEmptyMessage(message, uninterestedMessageID)
}

func EncodeHave(pieceIndex uint32) ([]byte, error) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, pieceIndex)

	return EncodeLengthPrefixed(LengthPrefixed{
		Length:    [4]byte{0, 0, 0, 5},
		MessageID: haveMessageID,
		Payload:   payload,
	})
}

func DecodeHave(message []byte) (uint32, error) {
	frame, err := DecodeLengthPrefixed(message)
	if err != nil {
		return 0, err
	}

	if frame.MessageID != haveMessageID || len(frame.Payload) != 4 {
		return 0, fmt.Errorf("message does not meet 'have' specifications")
	}

	return binary.BigEndian.Uint32(frame.Payload), nil
}

func EncodeRequest(request BlockRequest) ([]byte, error) {
	return encodeBlockRequest(request, requestMessageID)
}

func DecodeRequest(message []byte) (BlockRequest, error) {
	return decodeBlockRequest(message, requestMessageID)
}

func EncodeCancel(request BlockRequest) ([]byte, error) {
	return encodeBlockRequest(request, cancelMessageID)
}

func DecodeCancel(message []byte) (BlockRequest, error) {
	return decodeBlockRequest(message, cancelMessageID)
}

func EncodePiece(piece Piece) ([]byte, error) {
	if uint64(len(piece.Block))+9 > uint64(^uint32(0)) {
		return nil, fmt.Errorf("piece exceeds maximum frame size")
	}
	payload := make([]byte, 8+len(piece.Block))
	binary.BigEndian.PutUint32(payload[0:4], piece.Index)
	binary.BigEndian.PutUint32(payload[4:8], piece.Begin)
	copy(payload[8:], piece.Block)
	return encodeMessage(pieceMessageID, payload)
}

func DecodePiece(message []byte) (Piece, error) {
	frame, err := DecodeLengthPrefixed(message)
	if err != nil {
		return Piece{}, err
	}
	if frame.MessageID != pieceMessageID || len(frame.Payload) < 8 {
		return Piece{}, fmt.Errorf("message does not meet 'piece' specifications")
	}
	return Piece{
		Index: binary.BigEndian.Uint32(frame.Payload[0:4]),
		Begin: binary.BigEndian.Uint32(frame.Payload[4:8]),
		Block: frame.Payload[8:],
	}, nil
}

// EncodeBitfield encodes packed bits, with piece zero in the high bit of byte zero.
// pieceCount is needed to validate the byte count and unused trailing bits.
func EncodeBitfield(bitfield []byte, pieceCount int) ([]byte, error) {
	if err := validateBitfield(bitfield, pieceCount); err != nil {
		return nil, err
	}
	return encodeMessage(bitfieldMessageID, bitfield)
}

// DecodeBitfield returns packed availability bits independent of message's storage.
func DecodeBitfield(message []byte, pieceCount int) ([]byte, error) {
	frame, err := DecodeLengthPrefixed(message)
	if err != nil {
		return nil, err
	}
	if frame.MessageID != bitfieldMessageID {
		return nil, fmt.Errorf("unexpected message ID %d; want %d", frame.MessageID, bitfieldMessageID)
	}
	if err := validateBitfield(frame.Payload, pieceCount); err != nil {
		return nil, err
	}
	return frame.Payload, nil
}

func validateBitfield(bitfield []byte, pieceCount int) error {
	if pieceCount < 0 {
		return fmt.Errorf("piece count must not be negative")
	}
	byteCount := pieceCount / 8
	remainder := pieceCount % 8
	if remainder != 0 {
		byteCount++
	}
	if len(bitfield) != byteCount {
		return fmt.Errorf("bitfield length = %d; want %d", len(bitfield), byteCount)
	}
	if remainder != 0 && bitfield[len(bitfield)-1]&byte(0xff>>uint(remainder)) != 0 {
		return fmt.Errorf("unused bitfield bits must be zero")
	}
	return nil
}

func encodeBlockRequest(request BlockRequest, messageID byte) ([]byte, error) {
	payload := make([]byte, 12)
	binary.BigEndian.PutUint32(payload[0:4], request.Index)
	binary.BigEndian.PutUint32(payload[4:8], request.Begin)
	binary.BigEndian.PutUint32(payload[8:12], request.Length)
	return encodeMessage(messageID, payload)
}

func decodeBlockRequest(message []byte, messageID byte) (BlockRequest, error) {
	frame, err := DecodeLengthPrefixed(message)
	if err != nil {
		return BlockRequest{}, err
	}
	if frame.MessageID != messageID || len(frame.Payload) != 12 {
		return BlockRequest{}, fmt.Errorf("message does not meet block request specifications for ID %d", messageID)
	}
	return BlockRequest{
		Index:  binary.BigEndian.Uint32(frame.Payload[0:4]),
		Begin:  binary.BigEndian.Uint32(frame.Payload[4:8]),
		Length: binary.BigEndian.Uint32(frame.Payload[8:12]),
	}, nil
}

func encodeMessage(messageID byte, payload []byte) ([]byte, error) {
	length := uint64(len(payload)) + 1
	if length > uint64(^uint32(0)) {
		return nil, fmt.Errorf("payload exceeds maximum frame size")
	}
	var prefix [4]byte
	binary.BigEndian.PutUint32(prefix[:], uint32(length))
	return EncodeLengthPrefixed(LengthPrefixed{
		Length:    prefix,
		MessageID: messageID,
		Payload:   payload,
	})
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
