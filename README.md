# BitTorrent in Go

A command-line downloader built to understand how peer-to-peer file sharing works.
It finds computers sharing a file, downloads pieces from them in parallel, checks
each piece for corruption, and saves the finished result.

**Verified with a complete 793 MB Debian ISO download:** all 3,024 pieces passed
verification, and the finished file matched Debian's published checksum.
[See the verification record](docs/live-verification.md).

## What's built here

The torrent parser, peer communication, download coordination, and file assembly
are implemented here using Go's standard library, with no external dependencies.

- **Parallel downloads:** peers share the work without claiming the same piece.
- **Recovery from peer failures:** failed or corrupt pieces become available for
  another peer to download.
- **File handling:** supports torrents containing one file or several files,
  checks output paths, and refuses to overwrite existing downloads.
- **Tests for unreliable connections:** covers corrupt data, interrupted downloads,
  stalled peers, and pieces that arrive out of order.

## Try it

Install Go 1.26.7 or newer and clone this repository. From its directory, run:

```powershell
go run . -o downloads path/to/file.torrent
```

Replace `path/to/file.torrent` with your torrent file or an HTTP(S) torrent URL.
Put all options before the file path or URL.

| Option | Purpose |
| --- | --- |
| `-o downloads` | Choose the output directory; defaults to the current directory. |
| `-inspect` | Show which pieces peers have without downloading the content. |
| `-timeout 60s` | Allow a peer up to 60 seconds without useful data; defaults to 30 seconds. |

The CLI reports progress and the saved path. Output uses the torrent's name,
with a `.part` suffix until it finishes. Press Ctrl+C to cancel; partial output
is kept. Resume is not supported, so use a new output directory to retry.

## Tests

```powershell
go test ./... -cover
go vet ./...
```

Tests run locally without public peers. For race detection, use
`go test -race ./...` with cgo enabled and a C compiler installed.

## Scope

This is a learning project supporting BitTorrent v1, HTTP(S) trackers, and TCP
peer connections. It uses the torrent's primary tracker and gets its peer list
once per download.

Magnet links, DHT, UDP/WebSocket trackers, web seeds, peer exchange,
uploading/seeding, and resume are not implemented.

For a closer look at the code, read the
[download walkthrough](internal/download/README.md).
