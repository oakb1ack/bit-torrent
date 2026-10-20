# Tracking, downloading, and saving pieces (Steps 6–8)

Read `piece.go`, `scheduler.go`, `worker.go`, `coordinator.go`, and `store.go`,
in that order. Their tests demonstrate each part independently.

`Piece` describes one job: its zero-based index, expected SHA-1 hash, and byte
length. For a 10-byte file with a piece length of 4, the jobs have lengths 4, 4,
and 2. The constructor splits the torrent's `pieces` string into 20-byte raw
hashes and rejects metadata whose hash count disagrees with the file size.

Each job moves through these states:

```text
pending --Next--> assigned --Complete--> complete
   ^                 |
   +------Retry------+
```

`Next(connection.HasPiece)` finds a pending piece that the peer can supply and
marks it assigned before returning. Other workers then skip that piece.
The scheduler rotates its search position so a failed piece does not keep
getting selected ahead of all other work.

`Retry(piece.Index)` makes an assigned piece available again after a failed
attempt. The worker must stop its old attempt before returning the job.

`Complete(piece.Index)` marks an assigned piece finished. The coordinator calls
this only after a worker has verified the entire piece and storage has accepted
it. The scheduler stores the expected hash; the worker verifies received data.

`Progress()` returns completed and total counts. `Done()` becomes true when every
piece is complete. A false result from `Next` only means this peer has no eligible
pending work right now: other pieces may be assigned to workers or unavailable
from this particular peer. `Changes()` provides a notification channel that
closes when work is retried or completed, waking workers without a busy loop.
Take the notification snapshot before calling `Next` to avoid missing a retry
between checking for work and going to sleep.

The mutex makes selecting a piece and marking it assigned one indivisible
operation. Without it, two workers could both see the same pending piece before
either changes its state. Actual network I/O happens after `Next` returns and
the mutex has been released, so a slow peer will not hold up other workers.
The availability callback must only inspect the peer's bitfield; it must not
perform network I/O or call scheduler methods.

All workers share one scheduler pointer. Each worker owns its peer connection,
including changes to that peer's bitfield. The scheduler's mutex protects its
own bookkeeping; it does not protect peer connections.

`main.go` constructs the scheduler from torrent metadata and passes initialized
peers to `Run` in `coordinator.go`.

The worker sends `interested`, waits for the remote peer to unchoke it, and requests
up to eight blocks at a time. Each block is at most 16 KiB. A reader goroutine
delivers complete frames while the worker handles choke changes, keep-alives,
availability updates, and piece data. A timeout cancels a stalled peer without
discarding a partially read TCP frame and attempting to parse it as a new one.

`downloadPiece` checks block offsets, lengths, requested ranges, and duplicates.
It handles out-of-order blocks and reissues missing requests after a choke/unchoke
cycle. New block data extends the progress deadline; keep-alives and duplicate
blocks do not. A complete piece is accepted only if its SHA-1 matches the torrent.

The coordinator runs one worker per peer and receives verified pieces through a
channel. It calls storage serially, then marks the piece complete and reports
progress. Each worker waits for this acknowledgement before claiming more work,
so storage cannot build up an unbounded queue of downloaded pieces. Peer failures
return assigned work for another peer; a storage error cancels the whole download.
Cancellation closes sockets, releases assigned work, and waits for workers to exit.

`store.go` writes piece `i` at logical offset `i * pieceLength`. In a multi-file
torrent, that logical byte stream is split across files in torrent order; a piece
may span several files. Output paths are checked and filesystem access is confined
with `os.Root`. Data is staged under `.part`, synced, and published only after all
pieces have been written. Single-file publication uses a hard link to avoid
replacing an existing destination, so the output filesystem must support hard
links (for example, NTFS or ext4).

The tests demonstrate metadata parsing, peer availability filtering, retries,
completion, and concurrent workers claiming every piece exactly once. Run them
with:

```powershell
go test ./internal/download -v
```
