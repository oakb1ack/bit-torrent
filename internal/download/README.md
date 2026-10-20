# How downloads work

This package divides a torrent into pieces, downloads them from multiple peers,
and writes verified data to disk. A piece counts as complete only after its hash
matches the torrent metadata and its data has been written successfully.

## Follow a piece through the code

| File | Responsibility |
| --- | --- |
| [piece.go](piece.go) | Describes each piece's index, size, and expected hash. |
| [scheduler.go](scheduler.go) | Assigns work and tracks pending, assigned, and completed pieces. |
| [worker.go](worker.go) | Downloads from one peer and verifies each piece. |
| [coordinator.go](coordinator.go) | Runs workers, passes verified pieces to storage, and reports progress. |
| [store.go](store.go) | Places pieces in the right files and finishes the output. |

Each peer gets one worker. A shared scheduler prevents two workers from claiming
the same pending piece. If a peer fails or sends corrupt data, its unfinished
piece returns to the queue for another peer.

A worker requests up to eight blocks at a time, each at most 16 KiB. It handles
blocks arriving out of order and peers pausing transfers. Only new block data
extends the timeout; repeated data and keep-alive messages cannot keep a stalled
download running forever.

The coordinator writes pieces one at a time. Each worker waits for its piece to
be stored before taking more work, which limits the amount of data waiting to be
written. A storage error stops the download. Cancellation closes connections and
waits for workers to finish.

## Saving the result

Pieces may arrive in any order. Storage writes each one at its correct position,
including when a piece spans several files. It checks torrent paths and uses
`os.Root` to keep file access inside the output directory.

Output stays under a `.part` name until every piece is written and the files are
synced. Existing output is not reused, and incomplete output is kept for
inspection. Resume is not supported. Finishing a single-file download requires a
filesystem that supports hard links, such as NTFS or ext4.

## Notes for changes

- Share one scheduler pointer. Each worker owns its peer connection and availability data.
- Stop a failed attempt before calling `Retry`. Call `Complete` only after verification and storage succeed.
- `Next` returning no work does not mean the download is finished; use `Done` for that.
- Read `Changes` before calling `Next` to avoid missing a notification while waiting.
- The availability callback runs under the scheduler lock; it must not do network I/O or call the scheduler.

Run the package tests with `go test ./internal/download -v`.
For CLI usage, see the [project README](../../README.md).
