# Live download verification

On October 20, 2026 (UTC), the CLI downloaded a complete Debian installer from
public peers. The finished file matched Debian's published SHA-256 checksum.
This checked the full path from finding peers to assembling the saved file.

| Result | Recorded value |
| --- | --- |
| File | `debian-13.7.0-amd64-netinst.iso` |
| Size | 792,723,456 bytes (about 793 MB) |
| Peers | 42 returned by the tracker; 15 initialized successfully |
| Pieces | All 3,024 downloaded and SHA-1-verified |
| Exit status | 0, with an empty error log |

## Command and checksum

The recorded run used this command from the repository root:

```powershell
go run . -o test-results/steps7-8-live/output test-results/steps7-8-live/debian-netinst.torrent
```

The saved file was independently hashed with SHA-256 and compared with Debian's
checksum list. Both values were:

```text
a7ef94ac2fb9a7fec454552abd629b7cc9d5155c886165a45649f5ce6167e355
```

Sources used: Debian's [torrent](https://cdimage.debian.org/debian-cd/current/amd64/bt-cd/debian-13.7.0-amd64-netinst.iso.torrent)
and [checksum list](https://cdimage.debian.org/debian-cd/current/amd64/iso-cd/SHA256SUMS).
These URLs follow the current release and may change.

The ISO, torrent, logs, and checksum list are kept locally under
`test-results/steps7-8-live/`. That directory is ignored by Git; the artifacts
are not included in the repository.

## Automated checks

The test suite, `go vet ./...`, and a CLI build also passed. Download package
coverage was 86.1%. Tests cover corrupt data, retries, cancellation, timeouts,
and assembly of single-file and multi-file downloads.

The recorded Windows run did not include the race detector because cgo was
disabled and no C compiler was available on PATH.

See the [project README](../README.md) for setup and test commands.
