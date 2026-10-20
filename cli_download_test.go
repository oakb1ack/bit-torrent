package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
	"github.com/oakb1ack/bit-torrent/internal/tracker"
)

func TestCLIDownloadsAndVerifiesCompleteFile(t *testing.T) {
	data := bytes.Repeat([]byte("0123456789"), 5000)
	const pieceLength = 20000
	info := singleDownloadInfo("result.bin", data, pieceLength)
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error { return serveDownloadData(conn, hash, data, pieceLength) })
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"-o", dir, writeTestTorrent(t, announce, info)}, &out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "result.bin"))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("downloaded %d bytes, %v", len(got), err)
	}
	if _, err := os.Stat(filepath.Join(dir, "result.bin.part")); !os.IsNotExist(err) {
		t.Fatal("partial output remains")
	}
	if strings.Count(out.String(), "Downloaded and verified piece ") != 3 || !strings.Contains(out.String(), "Saved ") {
		t.Fatalf("unexpected progress:\n%s", out.String())
	}
}

func TestCLIDownloadsMultipleFilesAcrossPieceBoundaries(t *testing.T) {
	data := []byte("abcdefghij")
	info := "d5:filesld6:lengthi3e4:pathl5:a.txteed6:lengthi5e4:pathl6:nested5:b.txteed6:lengthi2e4:pathl5:c.txteee4:name6:bundle12:piece lengthi4e6:pieces" + bencodedString(downloadHashes(data, 4)) + "e"
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error { return serveDownloadData(conn, hash, data, 4) })
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	dir := t.TempDir()
	if err := run([]string{"-o", dir, writeTestTorrent(t, announce, info)}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"a.txt": "abc", filepath.Join("nested", "b.txt"): "defgh", "c.txt": "ij"} {
		got, err := os.ReadFile(filepath.Join(dir, "bundle", path))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, err)
		}
	}
}

func TestCLIFailedDownloadKeepsOnlyPartialOutput(t *testing.T) {
	data := []byte("never received")
	info := singleDownloadInfo("partial.bin", data, 4)
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if err := initializeDownloadPeer(conn, hash, len(data), 4); err != nil {
			return err
		}
		frame, err := readDownloadFrame(conn)
		if err != nil {
			return err
		}
		if err := peerprotocol.DecodeInterested(frame); err != nil {
			return err
		}
		unchoke, _ := peerprotocol.EncodeUnchoke()
		if _, err := conn.Write(unchoke); err != nil {
			return err
		}
		_, err = readDownloadFrame(conn)
		return err // Disconnect before returning data.
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	dir := t.TempDir()
	err := run([]string{"-o", dir, writeTestTorrent(t, announce, info)}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "download incomplete") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "partial.bin")); !os.IsNotExist(err) {
		t.Fatal("published failed download")
	}
	if _, err := os.Stat(filepath.Join(dir, "partial.bin.part")); err != nil {
		t.Fatal("missing partial output", err)
	}
}

func TestCLICancellationClosesConnections(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	data := []byte("cancel me")
	info := singleDownloadInfo("cancel.bin", data, 4)
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if err := initializeDownloadPeer(conn, hash, len(data), 4); err != nil {
			return err
		}
		frame, err := readDownloadFrame(conn)
		if err != nil {
			return err
		}
		if err := peerprotocol.DecodeInterested(frame); err != nil {
			return err
		}
		cancel()
		return waitForClientClose(conn)
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	err := runContext(ctx, []string{"-o", t.TempDir(), writeTestTorrent(t, announce, info)}, io.Discard)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestCLIDoesNotOverwriteExistingOutput(t *testing.T) {
	data := []byte("new data")
	info := singleDownloadInfo("existing", data, 4)
	hash := sha1.Sum([]byte(info))
	remote := startMainTestPeer(t, func(conn net.Conn) error {
		if err := initializeDownloadPeer(conn, hash, len(data), 4); err != nil {
			return err
		}
		return waitForClientClose(conn)
	})
	announce := testTracker(t, []tracker.Peer{remote}, nil)
	dir := t.TempDir()
	path := filepath.Join(dir, "existing")
	if err := os.WriteFile(path, []byte("original data"), 0600); err != nil {
		t.Fatal(err)
	}
	err := run([]string{"-o", dir, writeTestTorrent(t, announce, info)}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("error = %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original data" {
		t.Fatal("existing output changed")
	}
}

func TestCLIRejectsUnsafeFilenameBeforeNetworking(t *testing.T) {
	info := singleDownloadInfo("../escape", []byte("data"), 4)
	err := run([]string{"-o", t.TempDir(), writeTestTorrent(t, "http://127.0.0.1:1/announce", info)}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unsafe torrent name") {
		t.Fatalf("error = %v", err)
	}
}

func TestCLIEmptyDownloadNeedsNoPeers(t *testing.T) {
	info := singleDownloadInfo("empty", nil, 4)
	dir := t.TempDir()
	if err := run([]string{"-o", dir, writeTestTorrent(t, "http://127.0.0.1:1/announce", info)}, io.Discard); err != nil {
		t.Fatal(err)
	}
	file, err := os.Stat(filepath.Join(dir, "empty"))
	if err != nil || file.Size() != 0 {
		t.Fatalf("empty output = %v, %v", file, err)
	}
}

func singleDownloadInfo(name string, data []byte, pieceLength int) string {
	return fmt.Sprintf("d6:lengthi%de4:name%s12:piece lengthi%de6:pieces%se", len(data), bencodedString(name), pieceLength, bencodedString(downloadHashes(data, pieceLength)))
}

func downloadHashes(data []byte, pieceLength int) string {
	var hashes []byte
	for offset := 0; offset < len(data); offset += pieceLength {
		hash := sha1.Sum(data[offset:min(offset+pieceLength, len(data))])
		hashes = append(hashes, hash[:]...)
	}
	return string(hashes)
}

func bencodedString(value string) string { return fmt.Sprintf("%d:%s", len(value), value) }

func initializeDownloadPeer(conn net.Conn, hash [20]byte, length, pieceLength int) error {
	if _, err := readClientHandshake(conn, hash); err != nil {
		return err
	}
	if err := sendPeerHandshake(conn, hash); err != nil {
		return err
	}
	count := (length + pieceLength - 1) / pieceLength
	bits := make([]byte, (count+7)/8)
	for i := range count {
		bits[i/8] |= byte(0x80 >> uint(i%8))
	}
	frame, _ := peerprotocol.EncodeBitfield(bits, count)
	_, err := conn.Write(frame)
	return err
}

func serveDownloadData(conn net.Conn, hash [20]byte, data []byte, pieceLength int) error {
	if err := initializeDownloadPeer(conn, hash, len(data), pieceLength); err != nil {
		return err
	}
	frame, err := readDownloadFrame(conn)
	if err != nil {
		return err
	}
	if err := peerprotocol.DecodeInterested(frame); err != nil {
		return err
	}
	unchoke, _ := peerprotocol.EncodeUnchoke()
	if _, err := conn.Write(unchoke); err != nil {
		return err
	}
	for {
		frame, err := readDownloadFrame(conn)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		r, err := peerprotocol.DecodeRequest(frame)
		if err != nil {
			return err
		}
		start := int(r.Index)*pieceLength + int(r.Begin)
		end := start + int(r.Length)
		if r.Length == 0 || r.Length > 16384 || start < 0 || end > len(data) {
			return fmt.Errorf("invalid block request %#v", r)
		}
		frame, _ = peerprotocol.EncodePiece(peerprotocol.Piece{Index: r.Index, Begin: r.Begin, Block: data[start:end]})
		if _, err := conn.Write(frame); err != nil {
			return err
		}
	}
}

func readDownloadFrame(conn net.Conn) ([]byte, error) {
	var prefix [4]byte
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(prefix[:])
	if length > 1<<20 {
		return nil, fmt.Errorf("oversized frame")
	}
	frame := make([]byte, 4+int(length))
	copy(frame, prefix[:])
	_, err := io.ReadFull(conn, frame[4:])
	return frame, err
}
