# Piece tracking (Step 6)

Read `piece.go`, then `scheduler.go`, then `scheduler_test.go`.

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

`Complete(piece.Index)` marks an assigned piece finished. In Step 7 the worker
will call this only after downloading the entire piece and checking its SHA-1
hash. For now the scheduler stores the expected hash; it does not fetch data or
verify data itself.

`Progress()` returns completed and total counts. `Done()` becomes true when every
piece is complete. A false result from `Next` only means this peer has no eligible
pending work right now: other pieces may be assigned to workers or unavailable
from this particular peer. Step 7's coordinator will need to handle waiting or
stopping; a tight loop calling `Next` would waste CPU.

The mutex makes selecting a piece and marking it assigned one indivisible
operation. Without it, two workers could both see the same pending piece before
either changes its state. Actual network I/O happens after `Next` returns and
the mutex has been released, so a slow peer will not hold up other workers.
The availability callback must only inspect the peer's bitfield; it must not
perform network I/O or call scheduler methods.

All workers share one scheduler pointer. Each worker owns its peer connection,
including changes to that peer's bitfield. The scheduler's mutex protects its
own bookkeeping; it does not protect peer connections.

`main.go` now constructs the scheduler when loading torrent metadata and uses its
piece count for peer bitfield validation. Starting download workers belongs to
Step 7; the command still reports peer availability and closes connections on
exit.

The tests demonstrate metadata parsing, peer availability filtering, retries,
completion, and concurrent workers claiming every piece exactly once. Run them
with:

```powershell
go test ./internal/download -v
```
