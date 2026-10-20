package download

import (
	"bytes"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peer"
	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
)

func TestCoordinatorRetriesCorruptPeerAndStoresVerifiedPieces(t *testing.T) {
	data := []byte("abcdefgh")
	hash0, hash1 := sha1.Sum(data[:4]), sha1.Sum(data[4:])
	s, err := NewScheduler(8, 4, string(hash0[:])+string(hash1[:]))
	if err != nil {
		t.Fatal(err)
	}
	badClaimed := make(chan struct{})
	bad := coordinatorPeer(t, []byte{0xc0}, 2, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		frame, err := readTestFrame(conn)
		if err != nil {
			return err
		}
		request, err := peerprotocol.DecodeRequest(frame)
		if err != nil {
			return err
		}
		close(badClaimed)
		block, _ := peerprotocol.EncodePiece(peerprotocol.Piece{Index: request.Index, Begin: request.Begin, Block: []byte("xxxx")})
		if err := writeTestFrame(conn, block); err != nil {
			return err
		}
		return waitDownloadClose(conn)
	})
	good := coordinatorPeer(t, []byte{0}, 2, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		<-badClaimed
		for _, index := range []uint32{0, 1} {
			frame, _ := peerprotocol.EncodeHave(index)
			if err := writeTestFrame(conn, frame); err != nil {
				return err
			}
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		for range 2 {
			frame, err := readTestFrame(conn)
			if err != nil {
				return err
			}
			r, err := peerprotocol.DecodeRequest(frame)
			if err != nil {
				return err
			}
			block, _ := peerprotocol.EncodePiece(peerprotocol.Piece{Index: r.Index, Begin: r.Begin, Block: data[int(r.Index)*4 : int(r.Index)*4+4]})
			if err := writeTestFrame(conn, block); err != nil {
				return err
			}
		}
		return waitDownloadClose(conn)
	})
	stored := make([]byte, 8)
	notifications := 0
	err = Run(context.Background(), s, []*peer.Connection{bad, good}, func(p Piece, b []byte) error {
		if sha1.Sum(b) != p.Hash {
			return fmt.Errorf("unverified data reached storage")
		}
		copy(stored[p.Index*4:], b)
		return nil
	}, Options{Timeout: time.Second, OnPiece: func(Piece) error { notifications++; return nil }})
	if err != nil || !bytes.Equal(stored, data) || notifications != 2 || !s.Done() {
		t.Fatalf("Run = %v, stored %q, notifications %d", err, stored, notifications)
	}
}

func TestCoordinatorStopsOnStorageError(t *testing.T) {
	data := []byte("data")
	hash := sha1.Sum(data)
	s, _ := NewScheduler(4, 4, string(hash[:]))
	p := coordinatorPeer(t, []byte{0x80}, 1, serveOnePiece(data))
	err := Run(context.Background(), s, []*peer.Connection{p}, func(Piece, []byte) error { return io.ErrShortWrite }, Options{Timeout: time.Second})
	if !errors.Is(err, io.ErrShortWrite) || s.Done() {
		t.Fatalf("error = %v, done = %v", err, s.Done())
	}
	if _, ok := s.Next(func(int) bool { return true }); !ok {
		t.Fatal("failed piece was not returned for retry")
	}
}

func TestCoordinatorReportsUnavailablePieces(t *testing.T) {
	s := newTestScheduler(t, 1)
	p := coordinatorPeer(t, []byte{0}, 1, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		return waitDownloadClose(conn)
	})
	err := Run(context.Background(), s, []*peer.Connection{p}, func(Piece, []byte) error { return nil }, Options{Timeout: 50 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "download incomplete") {
		t.Fatalf("error = %v", err)
	}
}

func TestCoordinatorCancellationStopsBlockedPeer(t *testing.T) {
	s := newTestScheduler(t, 1)
	ready := make(chan struct{})
	p := coordinatorPeer(t, []byte{0x80}, 1, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		close(ready)
		return waitDownloadClose(conn)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-ready; cancel() }()
	err := Run(ctx, s, []*peer.Connection{p}, func(Piece, []byte) error { return nil }, Options{Timeout: time.Second})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if _, ok := s.Next(func(int) bool { return true }); !ok {
		t.Fatal("cancelled piece was not returned")
	}
}

func TestCoordinatorStopsOnProgressOutputError(t *testing.T) {
	data := []byte("data")
	hash := sha1.Sum(data)
	s, _ := NewScheduler(4, 4, string(hash[:]))
	p := coordinatorPeer(t, []byte{0x80}, 1, serveOnePiece(data))
	err := Run(context.Background(), s, []*peer.Connection{p}, func(Piece, []byte) error { return nil }, Options{Timeout: time.Second, OnPiece: func(Piece) error { return io.ErrClosedPipe }})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("error = %v", err)
	}
}

func TestCoordinatorRejectsMissingPeers(t *testing.T) {
	s := newTestScheduler(t, 1)
	if err := Run(context.Background(), s, nil, func(Piece, []byte) error { return nil }, Options{}); err == nil {
		t.Fatal("accepted incomplete download with no peers")
	}
}

func serveOnePiece(data []byte) func(net.Conn) error {
	return func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		frame, err := readTestFrame(conn)
		if err != nil {
			return err
		}
		r, err := peerprotocol.DecodeRequest(frame)
		if err != nil {
			return err
		}
		block, _ := peerprotocol.EncodePiece(peerprotocol.Piece{Index: r.Index, Begin: r.Begin, Block: data})
		if err := writeTestFrame(conn, block); err != nil {
			return err
		}
		return waitDownloadClose(conn)
	}
}

func waitDownloadClose(conn net.Conn) error {
	var b [1]byte
	_, err := conn.Read(b[:])
	if errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("expected connection close, got %v", err)
}

func coordinatorPeer(t *testing.T, bits []byte, count int, serve func(net.Conn) error) *peer.Connection {
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
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		wire := make([]byte, 68)
		if _, err := io.ReadFull(conn, wire); err != nil {
			done <- err
			return
		}
		handshake, err := peerprotocol.DecodeHandshake(wire)
		if err != nil {
			done <- err
			return
		}
		handshake.PeerID = [20]byte{42}
		wire, _ = peerprotocol.EncodeHandshake(handshake)
		bitfield, err := peerprotocol.EncodeBitfield(bits, count)
		if err != nil {
			done <- err
			return
		}
		if err := writeTestFrame(conn, wire, bitfield); err != nil {
			done <- err
			return
		}
		done <- serve(conn)
	}()
	p, err := peer.Connect(listener.Addr().String(), [20]byte{}, [20]byte{1}, count)
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Conn.Close()
		listener.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("fake peer: %v", err)
			}
		case <-time.After(4 * time.Second):
			t.Error("peer did not close")
		}
	})
	return p
}
