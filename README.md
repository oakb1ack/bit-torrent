## Bit Torrent built in Go

An educational BitTorrent v1 downloader written in Go. It discovers peers through
HTTP(S) trackers, downloads over TCP, verifies every piece's SHA-1 hash, and
assembles single-file and multi-file torrents.

Download a local torrent file or an HTTP(S) torrent URL (flags go before the
torrent argument):

```powershell
go run . -o downloads path/to/file.torrent
```

The client attempts up to 32 peer connections at a time. Each connected peer gets
one download worker, with up to eight requests for 16 KiB blocks in flight. Pieces
can arrive out of order; only hash-verified pieces are written and marked complete.
Progress reports each downloaded piece, followed by the saved output path.

Output uses the torrent's name inside the selected directory. During downloading,
the file or directory ends in `.part`. Once all pieces are stored, that suffix is
removed. Existing output or `.part` paths are never reused. Incomplete downloads
remain available for inspection; resuming them is not implemented. Choose a new
output directory for a fresh attempt.

Press Ctrl+C to cancel. Connections close and partial output is preserved. If all
peers fail or the remaining pieces cannot be obtained, the command exits with an
error rather than reporting success. Adjust the time allowed without useful peer
data with `-timeout 60s`.

To inspect peers without downloading content:

```powershell
go run . -inspect path/to/file.torrent > peer-bitfields.txt
```

Use Go 1.26.7 or newer. Run tests and static checks with:

```powershell
go test ./... -cover
go vet ./...
```

`go test -race ./...` additionally checks for data races when a C compiler and cgo
are available. Tests use local trackers and peers and do not need public swarms.

Read [the download walkthrough](internal/download/README.md) for piece tracking,
worker coordination, and file assembly, and the [live verification record](docs/live-verification.md)
for the completed Debian download and checksum comparison.

This project implements the eight tutorial
steps; it does not implement magnet links, DHT, UDP/WebSocket trackers, web seeds,
peer exchange, uploading/seeding, or resume. It uses the torrent's primary
`announce` tracker and does not refresh the peer list during a download.
