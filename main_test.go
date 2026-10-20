package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
	"github.com/oakb1ack/bit-torrent/internal/tracker"
)

func TestRunConnectsPeersConcurrently(t *testing.T) {
	info := testInfo("180:" + strings.Repeat("x", 180))
	infoHash := sha1.Sum([]byte(info))
	peerIDs := make(chan [20]byte, 3)
	trackerIDs := make(chan [20]byte, 1)
	var connected atomic.Int32
	ready := make(chan struct{})
	peers := make([]tracker.Peer, 3)
	for i, bits := range [][]byte{{0x81, 0x80}, {0x40, 0}} {
		peers[i] = startMainTestPeer(t, func(conn net.Conn) error {
			handshake, err := readClientHandshake(conn, infoHash)
			if err != nil {
				return err
			}
			peerIDs <- handshake.PeerID
			// Each peer waits for the other connection before replying. A serial
			// implementation cannot finish either handshake successfully.
			if connected.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-time.After(2 * time.Second):
				return fmt.Errorf("peer connections were not concurrent")
			}
			if err := sendPeerHandshake(conn, infoHash); err != nil {
				return err
			}
			bitfield, err := peerprotocol.EncodeBitfield(bits, 9)
			if err != nil {
				return err
			}
			if _, err := conn.Write(bitfield); err != nil {
				return err
			}
			return waitForClientClose(conn)
		})
	}
	peers[2] = startMainTestPeer(t, func(conn net.Conn) error {
		handshake, err := readClientHandshake(conn, infoHash)
		if err != nil {
			return err
		}
		peerIDs <- handshake.PeerID
		if err := sendPeerHandshake(conn, [20]byte{99}); err != nil {
			return err
		}
		return waitForClientClose(conn)
	})
	announce := testTracker(t, peers, func(r *http.Request) {
		query := r.URL.Query()
		if query.Get("info_hash") != string(infoHash[:]) {
			t.Errorf("tracker info hash = %x; want %x", query.Get("info_hash"), infoHash)
		}
		if len(query.Get("peer_id")) != 20 || query.Get("left") != "9" || query.Get("compact") != "1" {
			t.Errorf("unexpected tracker query: %v", query)
		}
		var id [20]byte
		copy(id[:], query.Get("peer_id"))
		trackerIDs <- id
	})
	var out bytes.Buffer
	if err := run([]string{writeTestTorrent(t, announce, info)}, &out); err != nil {
		t.Fatal(err)
	}
	output := out.String()
	first := fmt.Sprintf("Peer 1 is ip: %s port: %d\nHas piece 0\nHas piece 7\nHas piece 8\n", peers[0].IP, peers[0].Port)
	second := fmt.Sprintf("Peer 2 is ip: %s port: %d\nHas piece 1\n", peers[1].IP, peers[1].Port)
	for _, want := range []string{"Got 3 peers\n", first, second, "Connection failed:", "info hash does not match", "Connected to 2 of 3 peers\n"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q:\n%s", want, output)
		}
	}
	if strings.Index(output, first) > strings.Index(output, second) {
		t.Errorf("peer results are not in tracker order:\n%s", output)
	}
	select {
	case trackerID := <-trackerIDs:
		for range 3 {
			select {
			case id := <-peerIDs:
				if id != trackerID {
					t.Errorf("handshake ID %x differs from tracker ID %x", id, trackerID)
				}
			default:
				t.Error("missing peer handshake")
			}
		}
	default:
		t.Error("tracker was not contacted")
	}
}

func TestRunRejectsInvalidPieces(t *testing.T) {
	for _, pieces := range []string{"", "6:piecesi1e", "6:pieces21:" + strings.Repeat("x", 21)} {
		t.Run(fmt.Sprintf("pieces_%q", pieces), func(t *testing.T) {
			info := "d6:lengthi9e4:name4:test12:piece lengthi1e" + pieces + "e"
			path := writeTestTorrent(t, "http://127.0.0.1:1/announce", info)
			if err := run([]string{path}, io.Discard); err == nil || !strings.Contains(err.Error(), "pieces") {
				t.Fatalf("run error = %v; want pieces validation error", err)
			}
		})
	}
}

func TestRunWithNoTrackerPeers(t *testing.T) {
	announce := testTracker(t, nil, nil)
	var out bytes.Buffer
	if err := run([]string{writeTestTorrent(t, announce, testInfo("180:"+strings.Repeat("x", 180)))}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Got 0 peers\nConnected to 0 of 0 peers\n") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

func TestRunWhenAllPeersFail(t *testing.T) {
	info := testInfo("180:" + strings.Repeat("x", 180))
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if _, err := readClientHandshake(conn, hash); err != nil {
			return err
		}
		if err := sendPeerHandshake(conn, [20]byte{99}); err != nil {
			return err
		}
		return waitForClientClose(conn)
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	var out bytes.Buffer
	err := run([]string{writeTestTorrent(t, announce, info)}, &out)
	if err == nil || !strings.Contains(err.Error(), "could not connect to any tracker peers") {
		t.Fatalf("run error = %v; want all-peers-failed error", err)
	}
	if !strings.Contains(out.String(), "Connected to 0 of 1 peers\n") {
		t.Fatalf("missing failure summary: %s", out.String())
	}
}

func TestRunClosesConnectionsOnOutputFailure(t *testing.T) {
	info := testInfo("180:" + strings.Repeat("x", 180))
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if _, err := readClientHandshake(conn, hash); err != nil {
			return err
		}
		if err := sendPeerHandshake(conn, hash); err != nil {
			return err
		}
		if _, err := conn.Write([]byte{0, 0, 0, 3, 5, 0x80, 0}); err != nil {
			return err
		}
		return waitForClientClose(conn)
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	err := run([]string{writeTestTorrent(t, announce, info)}, failPeerOutput{})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("run error = %v; want output error", err)
	}
}

func TestRunPeerWithoutBitfield(t *testing.T) {
	info := testInfo("180:" + strings.Repeat("x", 180))
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if _, err := readClientHandshake(conn, hash); err != nil {
			return err
		}
		if err := sendPeerHandshake(conn, hash); err != nil {
			return err
		}
		if _, err := conn.Write([]byte{0, 0, 0, 1, 1}); err != nil {
			return err
		}
		return waitForClientClose(conn)
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	var out bytes.Buffer
	if err := run([]string{writeTestTorrent(t, announce, info)}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No pieces available\nConnected to 1 of 1 peers\n") {
		t.Fatalf("unexpected empty availability output: %s", out.String())
	}
}

type failPeerOutput struct{}

func (failPeerOutput) Write(b []byte) (int, error) {
	if bytes.HasPrefix(b, []byte("Peer ")) {
		return 0, io.ErrClosedPipe
	}
	return len(b), nil
}

func testInfo(pieces string) string {
	return "d6:lengthi9e4:name4:test12:piece lengthi1e6:pieces" + pieces + "e"
}

func writeTestTorrent(t *testing.T, announce, info string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.torrent")
	data := fmt.Sprintf("d8:announce%d:%s4:info%se", len(announce), announce, info)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func testTracker(t *testing.T, peers []tracker.Peer, onRequest func(*http.Request)) string {
	t.Helper()
	var compact []byte
	for _, remote := range peers {
		compact = append(compact, remote.IP.To4()...)
		compact = binary.BigEndian.AppendUint16(compact, remote.Port)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onRequest != nil {
			onRequest(r)
		}
		fmt.Fprintf(w, "d8:intervali60e5:peers%d:%se", len(compact), compact)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

func readClientHandshake(conn net.Conn, hash [20]byte) (peerprotocol.Handshake, error) {
	var wire [68]byte
	if _, err := io.ReadFull(conn, wire[:]); err != nil {
		return peerprotocol.Handshake{}, err
	}
	handshake, err := peerprotocol.DecodeHandshake(wire[:])
	if err == nil && handshake.InfoHash != hash {
		err = fmt.Errorf("handshake info hash = %x; want %x", handshake.InfoHash, hash)
	}
	return handshake, err
}

func sendPeerHandshake(conn net.Conn, hash [20]byte) error {
	wire, err := peerprotocol.EncodeHandshake(peerprotocol.Handshake{
		ProtocolStringLength: 19,
		ProtocolString:       "BitTorrent protocol",
		InfoHash:             hash,
		PeerID:               [20]byte{42},
	})
	if err != nil {
		return err
	}
	_, err = conn.Write(wire)
	return err
}

func waitForClientClose(conn net.Conn) error {
	var b [1]byte
	_, err := conn.Read(b[:])
	if errors.Is(err, io.EOF) {
		return nil
	}
	return fmt.Errorf("expected client to close connection; got %v", err)
}

func startMainTestPeer(t *testing.T, serve func(net.Conn) error) tracker.Peer {
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
	address := listener.Addr().(*net.TCPAddr)
	return tracker.Peer{IP: address.IP, Port: uint16(address.Port)}
}
