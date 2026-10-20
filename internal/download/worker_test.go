package download

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peer"
	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
)

func TestWorkerDownloadsOutOfOrderBlocks(t *testing.T) {
	data := bytes.Repeat([]byte("piece data"), 4000)
	w := testWorker(t, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 0}, []byte{0, 0, 0, 5, 4, 0, 0, 0, 1}, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		requests := make([]peerprotocol.BlockRequest, 3)
		for i := range requests {
			frame, err := readTestFrame(conn)
			if err != nil {
				return err
			}
			requests[i], err = peerprotocol.DecodeRequest(frame)
			if err != nil {
				return err
			}
			if requests[i].Index != 0 || requests[i].Begin != uint32(i*blockSize) || requests[i].Length != uint32(min(blockSize, len(data)-i*blockSize)) {
				return fmt.Errorf("wrong request: %#v", requests[i])
			}
		}
		for _, i := range []int{2, 2, 0, 1} {
			r := requests[i]
			frame, _ := peerprotocol.EncodePiece(peerprotocol.Piece{Index: r.Index, Begin: r.Begin, Block: data[int(r.Begin):int(r.Begin+r.Length)]})
			if err := writeTestFrame(conn, frame); err != nil {
				return err
			}
		}
		return nil
	})
	got, err := w.downloadPiece(Piece{Index: 0, Length: len(data), Hash: sha1.Sum(data)})
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download = %d bytes, %v", len(got), err)
	}
	if !bytes.Equal(w.connection.Bitfield, []byte{0x40}) {
		t.Fatalf("have was not applied: %x", w.connection.Bitfield)
	}
}

func TestWorkerRejectsBadBlocksAndHashes(t *testing.T) {
	data := []byte("verified bytes")
	tests := []struct {
		name  string
		block peerprotocol.Piece
		want  string
	}{
		{"wrong piece", peerprotocol.Piece{Index: 1, Block: data}, "unrequested piece"},
		{"bad offset", peerprotocol.Piece{Begin: 1, Block: data}, "offset"},
		{"short block", peerprotocol.Piece{Block: data[:3]}, "block length"},
		{"bad hash", peerprotocol.Piece{Block: bytes.Repeat([]byte{'x'}, len(data))}, "SHA-1 mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := testWorker(t, func(conn net.Conn) error {
				if err := expectInterested(conn); err != nil {
					return err
				}
				if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 1}); err != nil {
					return err
				}
				if _, err := readTestFrame(conn); err != nil {
					return err
				}
				frame, _ := peerprotocol.EncodePiece(tt.block)
				return writeTestFrame(conn, frame)
			})
			if _, err := w.downloadPiece(Piece{Index: 0, Length: len(data), Hash: sha1.Sum(data)}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v; want %s", err, tt.want)
			}
		})
	}
}

func TestWorkerReissuesRequestsAfterChoke(t *testing.T) {
	data := []byte("after unchoke")
	w := testWorker(t, func(conn net.Conn) error {
		if err := expectInterested(conn); err != nil {
			return err
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		first, err := readTestFrame(conn)
		if err != nil {
			return err
		}
		if err := writeTestFrame(conn, []byte{0, 0, 0, 1, 0}, []byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		second, err := readTestFrame(conn)
		if err != nil {
			return err
		}
		if !bytes.Equal(first, second) {
			return fmt.Errorf("missing block was not reissued")
		}
		frame, _ := peerprotocol.EncodePiece(peerprotocol.Piece{Block: data})
		return writeTestFrame(conn, frame)
	})
	if got, err := w.downloadPiece(Piece{Length: len(data), Hash: sha1.Sum(data)}); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("download = %q, %v", got, err)
	}
}

func TestWorkerTimeoutAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ready := make(chan struct{})
			go func() { expectInterested(server); close(ready) }()
			w, err := newWorker(ctx, &peer.Connection{Conn: client, Bitfield: []byte{0}}, 1, 50*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			defer w.close()
			<-ready
			if cancelled {
				cancel()
			}
			_, err = w.downloadPiece(Piece{Length: 1})
			if err == nil {
				t.Fatal("expected timeout or cancellation")
			}
			if cancelled && err != context.Canceled {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func testWorker(t *testing.T, serve func(net.Conn) error) *worker {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan error, 1)
	server.SetDeadline(time.Now().Add(2 * time.Second))
	go func() { defer server.Close(); done <- serve(server) }()
	w, err := newWorker(context.Background(), &peer.Connection{Conn: client, Bitfield: []byte{0}}, 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		w.close()
		if err := <-done; err != nil {
			t.Errorf("fake peer: %v", err)
		}
	})
	return w
}

func expectInterested(conn net.Conn) error {
	frame, err := readTestFrame(conn)
	if err != nil {
		return err
	}
	return peerprotocol.DecodeInterested(frame)
}

func readTestFrame(conn net.Conn) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(prefix[:])
	if length > 1<<20 {
		return nil, fmt.Errorf("oversized test frame")
	}
	frame := make([]byte, 4+int(length))
	copy(frame, prefix[:])
	_, err := io.ReadFull(conn, frame[4:])
	return frame, err
}

func writeTestFrame(conn net.Conn, frames ...[]byte) error {
	for _, frame := range frames {
		if _, err := conn.Write(frame); err != nil {
			return err
		}
	}
	return nil
}
