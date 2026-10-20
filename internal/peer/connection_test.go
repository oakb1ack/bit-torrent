package peer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestConnectContextCancelsHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	address := startTestPeer(t, func(conn net.Conn) error {
		if err := expectHandshake(conn); err != nil {
			return err
		}
		cancel()
		return expectClosed(conn)
	})
	p, err := ConnectContext(ctx, address, testInfoHash, testLocalID, 9)
	if p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("ConnectContext = %v, %v", p, err)
	}
}

var (
	testInfoHash = [20]byte{1, 2, 3, 4}
	testLocalID  = [20]byte{5, 6, 7, 8}
	testRemoteID = [20]byte{9, 10, 11, 12}
)

func TestConnectHandshakeAndBitfield(t *testing.T) {
	address := startTestPeer(t, func(conn net.Conn) error {
		if err := expectHandshake(conn); err != nil {
			return err
		}
		// Deliberately fragment the handshake, keep-alive, and bitfield.
		wire := append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 0)
		wire = append(wire, 0, 0, 0, 3, 5, 0x81, 0x80)
		for _, b := range wire {
			if _, err := conn.Write([]byte{b}); err != nil {
				return err
			}
		}
		if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		// A successful Connect must leave a usable connection for downloading.
		var ping [1]byte
		if _, err := io.ReadFull(conn, ping[:]); err != nil {
			return err
		}
		if ping[0] != 0xaa {
			return fmt.Errorf("unexpected ping %x", ping)
		}
		return nil
	})
	p, err := Connect(address, testInfoHash, testLocalID, 9)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Conn.Close() })
	if p.PeerID != testRemoteID || !bytes.Equal(p.Bitfield, []byte{0x81, 0x80}) {
		t.Fatalf("remote state = %x, %x", p.PeerID, p.Bitfield)
	}
	for index := -1; index <= 16; index++ {
		want := index == 0 || index == 7 || index == 8
		if got := p.HasPiece(index); got != want {
			t.Errorf("HasPiece(%d) = %v; want %v", index, got, want)
		}
	}
	if err := p.Conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message, err := p.ReadMessage()
	if err != nil || !bytes.Equal(message, []byte{0, 0, 0, 1, 1}) {
		t.Fatalf("next message = %x, %v", message, err)
	}
	if _, err := p.Conn.Write([]byte{0xaa}); err != nil {
		t.Fatal(err)
	}
}

func TestConnectWithoutBitfieldPreservesFirstMessage(t *testing.T) {
	for _, id := range []byte{0, 1, 2, 3, 4} {
		t.Run(fmt.Sprintf("ID_%d", id), func(t *testing.T) {
			first := []byte{0, 0, 0, 1, id}
			if id == 4 {
				first = []byte{0, 0, 0, 5, 4, 0, 0, 0, 8}
			}
			next := []byte{0, 0, 0, 5, 4, 0, 0, 0, 7}
			address := startTestPeer(t, func(conn net.Conn) error {
				if err := expectHandshake(conn); err != nil {
					return err
				}
				wire := append(testHandshake(testInfoHash, testRemoteID), first...)
				wire = append(wire, next...)
				_, err := conn.Write(wire)
				return err
			})
			p, err := Connect(address, testInfoHash, testLocalID, 9)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { p.Conn.Close() })
			wantBits := []byte{0, 0}
			if id == 4 {
				wantBits[1] = 0x80
			}
			if !bytes.Equal(p.Bitfield, wantBits) {
				t.Fatalf("bitfield = %x; want %x", p.Bitfield, wantBits)
			}
			for _, want := range [][]byte{first, next} {
				got, err := p.ReadMessage()
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("message = %x, %v; want %x", got, err, want)
				}
			}
		})
	}
}

func TestConnectRejectsInvalidPeerData(t *testing.T) {
	badProtocol := testHandshake(testInfoHash, testRemoteID)
	badProtocol[1] = 'X'
	tests := []struct {
		name string
		wire []byte
		want string
	}{
		{"protocol length", []byte{18}, "protocol length"},
		{"protocol string", badProtocol, "unsupported protocol"},
		{"wrong hash", testHandshake([20]byte{99}, testRemoteID), "info hash"},
		{"short handshake", testHandshake(testInfoHash, testRemoteID)[:20], "read handshake"},
		{"short prefix", append(testHandshake(testInfoHash, testRemoteID), 0, 0), "initial availability"},
		{"short payload", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 3, 5, 0x80), "initial availability"},
		{"oversized frame", append(testHandshake(testInfoHash, testRemoteID), 0xff, 0xff, 0xff, 0xff), "exceeds limit"},
		{"bitfield length", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 2, 5, 0x80), "bitfield length"},
		{"bitfield spare bits", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 3, 5, 0, 1), "unused bitfield bits"},
		{"out of range have", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 5, 4, 0, 0, 0, 9), "piece index"},
		{"short have", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 1, 4), "have"},
		{"malformed control", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 2, 1, 0), "must not have a payload"},
		{"unsupported message", append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 1, 99), "unexpected initial message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address := startTestPeer(t, func(conn net.Conn) error {
				if err := expectHandshake(conn); err != nil {
					return err
				}
				if _, err := conn.Write(tt.wire); err != nil {
					return err
				}
				if strings.HasPrefix(tt.name, "short ") {
					return nil // Closing causes the incomplete read to fail.
				}
				return expectClosed(conn)
			})
			p, err := Connect(address, testInfoHash, testLocalID, 9)
			if p != nil || err == nil || !strings.Contains(err.Error(), tt.want) {
				if p != nil {
					p.Conn.Close()
				}
				t.Fatalf("Connect = %v, %v; want error containing %q", p, err, tt.want)
			}
		})
	}
}

func TestConnectInputAndDialErrors(t *testing.T) {
	for _, count := range []int{-1, int(^uint(0) >> 1)} {
		if p, err := Connect("invalid address", testInfoHash, testLocalID, count); p != nil || err == nil || !strings.Contains(err.Error(), "piece count") {
			t.Fatalf("piece count %d: connection = %v, error = %v", count, p, err)
		}
	}
	if p, err := Connect("invalid address", testInfoHash, testLocalID, 9); p != nil || err == nil || !strings.Contains(err.Error(), "connect to") {
		t.Fatalf("invalid address: connection = %v, error = %v", p, err)
	}
}

func TestInitializationTimeoutClosesSocket(t *testing.T) {
	for _, stage := range []string{"handshake", "availability"} {
		t.Run(stage, func(t *testing.T) {
			address := startTestPeer(t, func(conn net.Conn) error {
				if err := expectHandshake(conn); err != nil {
					return err
				}
				if stage == "availability" {
					if _, err := conn.Write(testHandshake(testInfoHash, testRemoteID)); err != nil {
						return err
					}
				}
				return expectClosed(conn) // Leave the client waiting until its deadline.
			})
			p, err := connect(address, testInfoHash, testLocalID, 9, 200*time.Millisecond)
			var netErr net.Error
			if p != nil || !errors.As(err, &netErr) || !netErr.Timeout() {
				t.Fatalf("Connect = %v, %v; want timeout", p, err)
			}
		})
	}
}

func TestInitializationClearsDeadline(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	done := make(chan error, 1)
	go func() {
		if err := expectHandshake(server); err != nil {
			done <- err
			return
		}
		_, err := server.Write(append(testHandshake(testInfoHash, testRemoteID), 0, 0, 0, 1, 5))
		done <- err
	}()
	recorded := &deadlineRecorder{Conn: client}
	p, err := initialize(recorded, testInfoHash, testLocalID, 0, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(recorded.deadlines) != 2 || recorded.deadlines[0].IsZero() || !recorded.deadlines[1].IsZero() {
		t.Fatalf("initialization deadlines = %v", recorded.deadlines)
	}
	if len(p.Bitfield) != 0 || p.HasPiece(0) {
		t.Fatal("zero-piece torrent has nonempty availability")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReadMessageFraming(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	client.SetDeadline(time.Now().Add(time.Second))
	done := make(chan error, 1)
	// Frames are written together; the reader must not consume the next frame.
	frames := [][]byte{{0, 0, 0, 0}, {0, 0, 0, 1, 1}, {0, 0, 1, 1, 5}}
	frames[2] = append(frames[2], bytes.Repeat([]byte{0xa5}, 256)...)
	go func() {
		_, err := server.Write(bytes.Join(frames, nil))
		done <- err
	}()
	for _, want := range frames {
		got, err := readMessage(client)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("readMessage = %x, %v; want %x", got, err, want)
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestHasPieceRejectsPaddingAndMissingStorage(t *testing.T) {
	p := &Connection{Bitfield: []byte{0xff, 0xff}, pieceCount: 9}
	if p.HasPiece(-1) || p.HasPiece(9) || p.HasPiece(15) || p.HasPiece(int(^uint(0)>>1)) {
		t.Fatal("HasPiece accepted an index outside the torrent")
	}
	p.Bitfield = nil
	if p.HasPiece(0) {
		t.Fatal("HasPiece accepted missing storage")
	}
}

type deadlineRecorder struct {
	net.Conn
	deadlines []time.Time
}

func (c *deadlineRecorder) SetDeadline(deadline time.Time) error {
	c.deadlines = append(c.deadlines, deadline)
	return c.Conn.SetDeadline(deadline)
}

func testHandshake(hash, id [20]byte) []byte {
	wire := make([]byte, 68)
	wire[0] = 19
	copy(wire[1:20], "BitTorrent protocol")
	copy(wire[28:48], hash[:])
	copy(wire[48:68], id[:])
	return wire
}

func expectHandshake(conn net.Conn) error {
	var wire [68]byte
	if _, err := io.ReadFull(conn, wire[:]); err != nil {
		return err
	}
	if !bytes.Equal(wire[:], testHandshake(testInfoHash, testLocalID)) {
		return fmt.Errorf("unexpected client handshake %x", wire)
	}
	return nil
}

func expectClosed(conn net.Conn) error {
	var b [1]byte
	_, err := conn.Read(b[:])
	if err == nil {
		return fmt.Errorf("client sent unexpected data instead of closing")
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("client did not close failed connection: %w", err)
	}
	return nil
}

func startTestPeer(t *testing.T, serve func(net.Conn) error) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			done <- err
			return
		}
		done <- serve(conn)
	}()
	t.Cleanup(func() {
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("fake peer: %v", err)
			}
		case <-time.After(4 * time.Second):
			t.Error("fake peer did not finish")
		}
	})
	return listener.Addr().String()
}

func TestReadMessageRejectsOversizedLengthBeforePayload(t *testing.T) {
	client, server := net.Pipe()
	t.Cleanup(func() { client.Close(); server.Close() })
	client.SetDeadline(time.Now().Add(time.Second))
	go func() {
		var prefix [4]byte
		binary.BigEndian.PutUint32(prefix[:], maxMessageLength+1)
		server.Write(prefix[:])
	}()
	if _, err := readMessage(client); err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("readMessage error = %v; want size limit error", err)
	}
}
