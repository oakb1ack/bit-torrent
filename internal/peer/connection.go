package peer

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
)

const (
	connectionTimeout = 10 * time.Second
	// Bound allocations from untrusted length prefixes, including large bitfields.
	maxMessageLength = 1 << 20
)

// Connection holds one initialized peer. Callers own Conn and must close it.
// Access to Connection is intended for one goroutine at a time.
type Connection struct {
	Conn           net.Conn
	PeerID         [20]byte // Remote peer's ID
	Bitfield       []byte
	pieceCount     int
	pendingMessage []byte
}

// Connect sends our handshake, validates the remote handshake, and reads initial
// availability. A peer that omits its bitfield starts with no known pieces.
func Connect(address string, infoHash [20]byte, peerId [20]byte, pieceCount int) (*Connection, error) {
	return connect(address, infoHash, peerId, pieceCount, connectionTimeout)
}

// ConnectContext permits cancellation while dialing or exchanging initial data.
func ConnectContext(ctx context.Context, address string, infoHash, peerID [20]byte, pieceCount int) (*Connection, error) {
	return connectContext(ctx, address, infoHash, peerID, pieceCount, connectionTimeout)
}

func connect(address string, infoHash, peerID [20]byte, pieceCount int, timeout time.Duration) (*Connection, error) {
	return connectContext(context.Background(), address, infoHash, peerID, pieceCount, timeout)
}

func connectContext(ctx context.Context, address string, infoHash, peerID [20]byte, pieceCount int, timeout time.Duration) (*Connection, error) {
	if _, err := bitfieldSize(pieceCount); err != nil {
		return nil, err
	}
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	p, err := initialize(conn, infoHash, peerID, pieceCount, timeout)
	if ctx.Err() != nil {
		conn.Close()
		return nil, ctx.Err()
	}
	return p, err
}

func initialize(conn net.Conn, infoHash, peerID [20]byte, pieceCount int, timeout time.Duration) (_ *Connection, err error) {
	defer func() {
		if err != nil {
			conn.Close()
		}
	}()
	byteCount, err := bitfieldSize(pieceCount)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("set initialization deadline: %w", err)
	}
	handshake, err := peerprotocol.EncodeHandshake(peerprotocol.Handshake{
		ProtocolStringLength: 19,
		ProtocolString:       "BitTorrent protocol",
		InfoHash:             infoHash,
		PeerID:               peerID,
	})
	if err != nil {
		return nil, fmt.Errorf("encode handshake: %w", err)
	}
	if n, err := conn.Write(handshake); err != nil {
		return nil, fmt.Errorf("send handshake: %w", err)
	} else if n != len(handshake) {
		return nil, fmt.Errorf("send handshake: %w", io.ErrShortWrite)
	}
	// Reading the length first lets us reject an unsupported protocol immediately.
	remote := make([]byte, 68)
	if _, err := io.ReadFull(conn, remote[:1]); err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	if remote[0] != 19 {
		return nil, fmt.Errorf("unsupported handshake protocol length %d", remote[0])
	}
	if _, err := io.ReadFull(conn, remote[1:]); err != nil {
		return nil, fmt.Errorf("read handshake: %w", err)
	}
	decoded, err := peerprotocol.DecodeHandshake(remote)
	if err != nil {
		return nil, fmt.Errorf("decode handshake: %w", err)
	}
	if decoded.InfoHash != infoHash {
		return nil, fmt.Errorf("peer handshake info hash does not match torrent")
	}
	p := &Connection{Conn: conn, PeerID: decoded.PeerID, Bitfield: make([]byte, byteCount), pieceCount: pieceCount}
	for {
		message, err := readMessage(conn)
		if err != nil {
			return nil, fmt.Errorf("read initial availability: %w", err)
		}
		if len(message) == 4 { // Keep-alive; the overall deadline still applies.
			continue
		}
		if err := p.readInitialMessage(message); err != nil {
			return nil, err
		}
		break
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear initialization deadline: %w", err)
	}
	return p, nil
}

func bitfieldSize(pieceCount int) (int, error) {
	if pieceCount < 0 {
		return 0, fmt.Errorf("piece count must not be negative")
	}
	size := pieceCount / 8
	if pieceCount%8 != 0 {
		size++
	}
	if size > maxMessageLength-1 {
		return 0, fmt.Errorf("piece count exceeds supported bitfield size")
	}
	return size, nil
}

func (p *Connection) readInitialMessage(message []byte) error {
	var err error
	switch message[4] {
	case 5:
		p.Bitfield, err = peerprotocol.DecodeBitfield(message, p.pieceCount)
		return err
	case 0:
		err = peerprotocol.DecodeChoke(message)
	case 1:
		err = peerprotocol.DecodeUnchoke(message)
	case 2:
		err = peerprotocol.DecodeInterested(message)
	case 3:
		err = peerprotocol.DecodeUninterested(message)
	case 4:
		var index uint32
		index, err = peerprotocol.DecodeHave(message)
		if err == nil {
			if uint64(index) >= uint64(p.pieceCount) {
				return fmt.Errorf("have piece index %d exceeds torrent piece count", index)
			}
			p.Bitfield[index/8] |= byte(0x80 >> (index % 8))
		}
	default:
		return fmt.Errorf("unexpected initial message ID %d", message[4])
	}
	if err != nil {
		return err
	}
	// The bitfield was omitted. Preserve this message for the download loop.
	p.pendingMessage = message
	return nil
}

// ReadMessage returns one complete frame, including its length prefix. It returns
// an initial non-bitfield message first, if one was read during Connect. Callers
// set their own deadlines and process later messages to update peer state.
func (p *Connection) ReadMessage() ([]byte, error) {
	if p.pendingMessage != nil {
		message := p.pendingMessage
		p.pendingMessage = nil
		return message, nil
	}
	return readMessage(p.Conn)
}

func readMessage(conn net.Conn) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(prefix[:])
	if length > maxMessageLength {
		return nil, fmt.Errorf("peer message length %d exceeds limit %d", length, maxMessageLength)
	}
	message := make([]byte, 4+int(length))
	copy(message, prefix[:])
	if _, err := io.ReadFull(conn, message[4:]); err != nil {
		return nil, err
	}
	return message, nil
}

func (p *Connection) HasPiece(index int) bool {
	return index >= 0 && index < p.pieceCount && index/8 < len(p.Bitfield) &&
		p.Bitfield[index/8]&byte(0x80>>uint(index%8)) != 0
}
