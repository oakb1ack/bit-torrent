package main

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/bencode"
	"github.com/oakb1ack/bit-torrent/internal/download"
	"github.com/oakb1ack/bit-torrent/internal/peer"
	"github.com/oakb1ack/bit-torrent/internal/tracker"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runContext(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	return runContext(context.Background(), args, out)
}

func runContext(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("bit-torrent", flag.ContinueOnError)
	flags.SetOutput(out)
	inspect := flags.Bool("inspect", false, "report peer availability without downloading")
	directory := flags.String("o", ".", "download directory")
	timeout := flags.Duration("timeout", 30*time.Second, "maximum time without useful peer data")
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: bit-torrent [-inspect] [-o directory] [-timeout 30s] <torrent file or URL>")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}

	torrentBytes, err := readTorrentContext(ctx, flags.Arg(0))
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
	pieces, ok := info["pieces"].(string)
	if !ok || len(pieces)%sha1.Size != 0 {
		return fmt.Errorf("info dictionary must contain pieces with a length divisible by %d", sha1.Size)
	}
	pieceLength, ok := info["piece length"].(int)
	if !ok {
		return fmt.Errorf("info dictionary must contain an integer piece length")
	}
	scheduler, err := download.NewScheduler(fileSize, pieceLength, pieces)
	if err != nil {
		return fmt.Errorf("initialize pieces: %w", err)
	}
	_, pieceCount := scheduler.Progress()
	var layout download.Layout
	if !*inspect {
		layout, err = torrentLayout(info, fileSize, pieceLength)
		if err != nil {
			return err
		}
		if scheduler.Done() {
			return saveDownload(ctx, scheduler, nil, *directory, layout, *timeout, out)
		}
	}

	infoHash := sha1.Sum(rawInfo)
	var peerID [20]byte
	copy(peerID[:], "-GO0001-")
	if _, err := rand.Read(peerID[8:]); err != nil {
		return fmt.Errorf("generate peer ID: %w", err)
	}

	peers, err := tracker.ShowPeersContext(ctx, announce, tracker.Parameters{
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
	results := connectPeersContext(ctx, peers, infoHash, peerID, pieceCount)
	defer func() {
		for _, result := range results {
			if result.connection != nil {
				result.connection.Conn.Close()
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	connected := 0
	var connections []*peer.Connection
	for i, result := range results {
		if _, err := fmt.Fprintf(out, "Peer %d is ip: %s port: %d\n", i+1, peers[i].IP, peers[i].Port); err != nil {
			return err
		}
		if result.err != nil {
			if _, err := fmt.Fprintf(out, "Connection failed: %v\n", result.err); err != nil {
				return err
			}
			continue
		}
		connected++
		connections = append(connections, result.connection)
		if !*inspect {
			continue
		}
		hasPieces := false
		for index := 0; index < pieceCount; index++ {
			if result.connection.HasPiece(index) {
				hasPieces = true
				if _, err := fmt.Fprintf(out, "Has piece %d\n", index); err != nil {
					return err
				}
			}
		}
		if !hasPieces {
			if _, err := fmt.Fprintln(out, "No pieces available"); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(out, "Connected to %d of %d peers\n", connected, len(peers)); err != nil {
		return err
	}
	if (len(peers) != 0 || !*inspect) && connected == 0 {
		return fmt.Errorf("could not connect to any tracker peers")
	}
	if !*inspect {
		return saveDownload(ctx, scheduler, connections, *directory, layout, *timeout, out)
	}
	return nil
}

func saveDownload(ctx context.Context, scheduler *download.Scheduler, connections []*peer.Connection, directory string, layout download.Layout, timeout time.Duration, out io.Writer) error {
	store, err := download.NewStore(directory, layout)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer store.Close()
	err = download.Run(ctx, scheduler, connections, store.WritePiece, download.Options{Timeout: timeout, OnPiece: func(piece download.Piece) error {
		completed, total := scheduler.Progress()
		_, err := fmt.Fprintf(out, "Downloaded and verified piece %d (%d/%d)\n", piece.Index, completed, total)
		return err
	}})
	if err != nil {
		return err
	}
	if err := store.Finish(); err != nil {
		return fmt.Errorf("finish output: %w", err)
	}
	_, err = fmt.Fprintf(out, "Saved %s\n", filepath.Join(directory, layout.Name))
	return err
}

func torrentLayout(info map[string]any, totalLength int64, pieceLength int) (download.Layout, error) {
	name, ok := info["name"].(string)
	if !ok {
		return download.Layout{}, fmt.Errorf("torrent must contain a filename")
	}
	layout := download.Layout{Name: name, Length: totalLength, PieceLength: pieceLength}
	_, single := info["length"]
	rawFiles, multiple := info["files"]
	if single == multiple {
		return layout, fmt.Errorf("torrent must specify exactly one of length or files")
	}
	if multiple {
		entries, ok := rawFiles.([]any)
		if !ok {
			return layout, fmt.Errorf("invalid files list")
		}
		layout.Files = make([]download.File, 0, len(entries))
		for _, entry := range entries {
			file, ok := entry.(map[string]any)
			if !ok {
				return layout, fmt.Errorf("invalid file entry")
			}
			length, ok := file["length"].(int)
			if !ok || length < 0 {
				return layout, fmt.Errorf("invalid file length")
			}
			parts, ok := file["path"].([]any)
			if !ok {
				return layout, fmt.Errorf("invalid file path")
			}
			path := make([]string, len(parts))
			for i, part := range parts {
				text, ok := part.(string)
				if !ok {
					return layout, fmt.Errorf("invalid file path component")
				}
				path[i] = text
			}
			layout.Files = append(layout.Files, download.File{Path: path, Length: int64(length)})
		}
	}
	return layout, layout.Validate()
}

const maxConcurrentPeers = 32

type peerResult struct {
	index      int
	connection *peer.Connection
	err        error
}

// connectPeers retains successful connections and their bitfields in tracker
// order. The caller owns the returned connections and must close them.
func connectPeers(peers []tracker.Peer, infoHash, peerID [20]byte, pieceCount int) []peerResult {
	return connectPeersContext(context.Background(), peers, infoHash, peerID, pieceCount)
}

func connectPeersContext(ctx context.Context, peers []tracker.Peer, infoHash, peerID [20]byte, pieceCount int) []peerResult {
	results := make([]peerResult, len(peers))
	jobs := make(chan int)
	completed := make(chan peerResult)
	var workers sync.WaitGroup
	for range min(maxConcurrentPeers, len(peers)) {
		workers.Go(func() {
			for index := range jobs {
				address := net.JoinHostPort(peers[index].IP.String(), strconv.Itoa(int(peers[index].Port)))
				connection, err := peer.ConnectContext(ctx, address, infoHash, peerID, pieceCount)
				completed <- peerResult{index: index, connection: connection, err: err}
			}
		})
	}
	go func() {
		for index := range peers {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		close(completed)
	}()
	for result := range completed {
		results[result.index] = result
	}
	return results
}

func readTorrent(source string) ([]byte, error) {
	return readTorrentContext(context.Background(), source)
}

func readTorrentContext(ctx context.Context, source string) ([]byte, error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return os.ReadFile(source)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
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
