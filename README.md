## Bit Torrent built in Go

Run with a local torrent file or an HTTP(S) torrent URL:

```powershell
go run . path/to/file.torrent
```

The client asks the tracker for peers, connects to up to 32 peers concurrently,
exchanges handshakes, and prints the pieces available from each peer. Failed
connections are reported individually. Connections close when the command exits.

Save the availability report to a file with:

```powershell
go run . path/to/file.torrent > peer-bitfields.txt
```

Run the automated tests with `go test ./...`.
