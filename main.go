package main

import (
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/oakb1ack/bit-torrent/internal/bencode"
	"github.com/oakb1ack/bit-torrent/internal/tracker"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: bit-torrent <torrent file or URL>")
	}

	torrentBytes, err := readTorrent(args[0])
	if err != nil {
		return fmt.Errorf("read torrent: %w", err)
	}
	decoded, err := bencode.Decode(string(torrentBytes))
	if err != nil {
		return fmt.Errorf("decode torrent: %w", err)
	}
	torrentFile, ok := decoded.(map[string]any)
	if !ok {
		return fmt.Errorf("torrent root is not a dictionary")
	}
	announce, ok := torrentFile["announce"].(string)
	if !ok || announce == "" {
		return fmt.Errorf("torrent does not contain a valid announce URL")
	}
	info, ok := torrentFile["info"].(map[string]any)
	if !ok {
		return fmt.Errorf("torrent does not contain a valid info dictionary")
	}

	rawInfo, err := dictionaryValue(torrentBytes, "info")
	if err != nil {
		return fmt.Errorf("find encoded info dictionary: %w", err)
	}
	fileSize, err := torrentSize(info)
	if err != nil {
		return err
	}

	infoHash := sha1.Sum(rawInfo)
	var peerID [20]byte
	copy(peerID[:], "-GO0001-")
	if _, err := rand.Read(peerID[8:]); err != nil {
		return fmt.Errorf("generate peer ID: %w", err)
	}

	peers, err := tracker.ShowPeers(announce, tracker.Parameters{
		InfoHash:   infoHash,
		PeerID:     peerID,
		Port:       6881,
		Uploaded:   0,
		Downloaded: 0,
		Left:       fileSize,
		Compact:    1,
	})
	if err != nil {
		return fmt.Errorf("contact tracker: %w", err)
	}

	if _, err := fmt.Fprintln(out, "200 OK"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Got %d peers\n", len(peers)); err != nil {
		return err
	}
	for i, peer := range peers {
		if _, err := fmt.Fprintf(out, "Peer %d is ip: %s port: %d\n", i, peer.IP, peer.Port); err != nil {
			return err
		}
	}
	return nil
}

func readTorrent(source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return os.ReadFile(source)
	}

	resp, err := http.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("torrent URL returned HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func torrentSize(info map[string]any) (int64, error) {
	if length, ok := info["length"].(int); ok && length >= 0 {
		return int64(length), nil
	}
	files, ok := info["files"].([]any)
	if !ok {
		return 0, fmt.Errorf("info dictionary has neither a valid length nor files list")
	}
	var total int64
	for _, item := range files {
		file, ok := item.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("torrent file entry is not a dictionary")
		}
		length, ok := file["length"].(int)
		if !ok || length < 0 {
			return 0, fmt.Errorf("torrent file entry has an invalid length")
		}
		total += int64(length)
	}
	return total, nil
}

// dictionaryValue returns the original bencoded bytes for a top-level value.
// The info hash is over those exact bytes, so re-encoding a decoded map is not safe.
func dictionaryValue(data []byte, wanted string) ([]byte, error) {
	if len(data) == 0 || data[0] != 'd' {
		return nil, fmt.Errorf("root value is not a dictionary")
	}
	for pos := 1; pos < len(data) && data[pos] != 'e'; {
		key, next, err := parseByteString(data, pos)
		if err != nil {
			return nil, err
		}
		start := next
		end, err := skipBencodedValue(data, start)
		if err != nil {
			return nil, err
		}
		if string(key) == wanted {
			return data[start:end], nil
		}
		pos = end
	}
	return nil, fmt.Errorf("dictionary does not contain %q", wanted)
}

func parseByteString(data []byte, pos int) ([]byte, int, error) {
	if pos >= len(data) || data[pos] < '0' || data[pos] > '9' {
		return nil, pos, fmt.Errorf("expected bencoded string at byte %d", pos)
	}
	length := 0
	for pos < len(data) && data[pos] >= '0' && data[pos] <= '9' {
		length = length*10 + int(data[pos]-'0')
		pos++
	}
	if pos >= len(data) || data[pos] != ':' {
		return nil, pos, fmt.Errorf("expected string delimiter")
	}
	start := pos + 1
	end := start + length
	if end > len(data) {
		return nil, pos, fmt.Errorf("bencoded string exceeds input")
	}
	return data[start:end], end, nil
}

func skipBencodedValue(data []byte, pos int) (int, error) {
	if pos >= len(data) {
		return pos, fmt.Errorf("unexpected end of bencoded value")
	}
	switch data[pos] {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		_, end, err := parseByteString(data, pos)
		return end, err
	case 'i':
		for i := pos + 1; i < len(data); i++ {
			if data[i] == 'e' {
				return i + 1, nil
			}
		}
		return pos, fmt.Errorf("unterminated bencoded integer")
	case 'l', 'd':
		kind := data[pos]
		if pos+1 < len(data) && data[pos+1] == 'e' {
			return pos + 2, nil
		}
		for cursor := pos + 1; cursor < len(data) && data[cursor] != 'e'; {
			if kind == 'd' {
				_, next, err := parseByteString(data, cursor)
				if err != nil {
					return pos, err
				}
				cursor = next
			}
			next, err := skipBencodedValue(data, cursor)
			if err != nil {
				return pos, err
			}
			cursor = next
			if cursor < len(data) && data[cursor] == 'e' {
				return cursor + 1, nil
			}
		}
		return pos, fmt.Errorf("unterminated bencoded container")
	default:
		return pos, fmt.Errorf("unsupported bencode type at byte %d", pos)
	}
}
