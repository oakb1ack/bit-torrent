# Steps 7 and 8 live verification

Completed on 2026-10-20 UTC using the actual CLI and public peers.

```powershell
go run . -o test-results/steps7-8-live/output test-results/steps7-8-live/debian-netinst.torrent
```

The torrent metadata came from [Debian's primary server](https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/debian-13.7.0-amd64-netinst.iso.torrent).
The tracker returned 42 peers; 15 completed initialization. The client downloaded
and SHA-1-verified all 3,024 pieces, then published the complete
`debian-13.7.0-amd64-netinst.iso` file (792,723,456 bytes). The process exited with
status zero and an empty error log.

After downloading, the complete saved file was independently SHA-256-hashed and
compared with [Debian's published SHA256SUMS](https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/SHA256SUMS).
Both values were:

```text
a7ef94ac2fb9a7fec454552abd629b7cc9d5155c886165a45649f5ce6167e355
```

This checks both piece verification and the final ordering/assembly of bytes.
The ISO, torrent metadata, download log, and downloaded checksum list remain
under the ignored `test-results/steps7-8-live/` directory in the development
workspace; these large artifacts are not committed.

Automated verification also passed:

```powershell
go test ./... -cover
go vet ./...
go build -o test-results/steps7-8-live/bit-torrent.exe .
```

The download package's statement coverage was 86.1%. Tests use local TCP peers
and HTTP trackers and cover corrupt pieces, retries, cancellation, choke changes,
pipeline refill, duplicates, timeouts, output errors, single-file assembly, and
multi-file boundaries. The race detector was unavailable in this Windows
environment because cgo is disabled and no C compiler is installed on PATH.
