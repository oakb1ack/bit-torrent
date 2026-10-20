package download

import (
	"crypto/sha1"
	"fmt"
)

// Piece describes the work needed to download one complete piece. Hash contains
// its expected SHA-1 bytes from the torrent, not the hash of downloaded data yet.
type Piece struct {
	Index  int
	Hash   [sha1.Size]byte
	Length int
}

func parsePieces(totalLength int64, pieceLength int, hashes string) ([]Piece, error) {
	if totalLength < 0 {
		return nil, fmt.Errorf("total length must not be negative")
	}
	if pieceLength <= 0 {
		return nil, fmt.Errorf("piece length must be positive")
	}
	if len(hashes)%sha1.Size != 0 {
		return nil, fmt.Errorf("pieces must contain whole %d-byte hashes", sha1.Size)
	}
	// Round up without adding to totalLength, which could overflow for large files.
	expectedCount := totalLength / int64(pieceLength)
	if totalLength%int64(pieceLength) != 0 {
		expectedCount++
	}
	count := len(hashes) / sha1.Size
	if int64(count) != expectedCount {
		return nil, fmt.Errorf("pieces contains %d hashes; file size and piece length require %d", count, expectedCount)
	}
	pieces := make([]Piece, count)
	for index := range pieces {
		length := pieceLength
		if index == count-1 {
			length = int(totalLength - int64(index)*int64(pieceLength))
		}
		pieces[index] = Piece{Index: index, Length: length}
		copy(pieces[index].Hash[:], hashes[index*sha1.Size:(index+1)*sha1.Size])
	}
	return pieces, nil
}
