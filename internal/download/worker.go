package download

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peer"
	"github.com/oakb1ack/bit-torrent/internal/peerprotocol"
)

const (
	blockSize       = 16 * 1024
	requestPipeline = 8
	maxPieceLength  = 64 * 1024 * 1024
)

type incoming struct {
	message []byte
	err     error
}

// One worker owns protocol state for one peer. A separate reader keeps TCP
// framing intact while the worker waits for either messages or scheduler changes.
type worker struct {
	connection      *peer.Connection
	pieceCount      int
	choked          bool
	timeout         time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	messages        chan incoming
	readerDone      chan struct{}
	stopClose       func() bool
	requestedPieces map[uint32]bool
}

func newWorker(ctx context.Context, connection *peer.Connection, pieceCount int, timeout time.Duration) (*worker, error) {
	ctx, cancel := context.WithCancel(ctx)
	w := &worker{connection: connection, pieceCount: pieceCount, choked: true, timeout: timeout,
		ctx: ctx, cancel: cancel, messages: make(chan incoming), readerDone: make(chan struct{}),
		requestedPieces: make(map[uint32]bool)}
	w.stopClose = context.AfterFunc(ctx, func() { connection.Conn.Close() })
	go func() {
		defer close(w.readerDone)
		for {
			message, err := connection.ReadMessage()
			select {
			case w.messages <- incoming{message, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	interested, _ := peerprotocol.EncodeInterested()
	if err := w.send(interested); err != nil {
		w.close()
		return nil, err
	}
	return w, nil
}

func (w *worker) close() {
	w.cancel()
	w.connection.Conn.Close()
	w.stopClose()
	<-w.readerDone
}

func (w *worker) send(message []byte) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if err := w.connection.Conn.SetWriteDeadline(time.Now().Add(w.timeout)); err != nil {
		return err
	}
	n, err := w.connection.Conn.Write(message)
	if err == nil && n != len(message) {
		err = io.ErrShortWrite
	}
	return err
}

func (w *worker) receive(deadline time.Time) ([]byte, error) {
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return nil, w.ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("peer made no download progress before timeout")
	case result := <-w.messages:
		return result.message, result.err
	}
}

// handle updates control state, or returns a block for the active download.
func (w *worker) handle(message []byte) (*peerprotocol.Piece, error) {
	if len(message) == 4 {
		return nil, peerprotocol.DecodeKeepAlive(message)
	}
	if len(message) < 5 {
		return nil, fmt.Errorf("peer message has no ID")
	}
	var err error
	switch message[4] {
	case 0:
		err = peerprotocol.DecodeChoke(message)
		w.choked = true
	case 1:
		err = peerprotocol.DecodeUnchoke(message)
		w.choked = false
	case 2:
		err = peerprotocol.DecodeInterested(message)
	case 3:
		err = peerprotocol.DecodeUninterested(message)
	case 4:
		var index uint32
		index, err = peerprotocol.DecodeHave(message)
		if err == nil {
			if uint64(index) >= uint64(w.pieceCount) || int(index/8) >= len(w.connection.Bitfield) {
				return nil, fmt.Errorf("have index %d is outside torrent", index)
			}
			w.connection.Bitfield[index/8] |= byte(0x80 >> (index % 8))
		}
	case 5:
		w.connection.Bitfield, err = peerprotocol.DecodeBitfield(message, w.pieceCount)
	case 6:
		// We remain choked toward the remote peer: uploading is not enabled.
		_, err = peerprotocol.DecodeRequest(message)
	case 7:
		var block peerprotocol.Piece
		block, err = peerprotocol.DecodePiece(message)
		return &block, err
	case 8:
		_, err = peerprotocol.DecodeCancel(message)
	default:
		err = fmt.Errorf("unsupported peer message ID %d", message[4])
	}
	return nil, err
}

type blockState struct {
	length    int
	requested bool
	sent      bool
	received  bool
}

func (w *worker) downloadPiece(piece Piece) ([]byte, error) {
	if piece.Index < 0 || uint64(piece.Index) > uint64(^uint32(0)) || piece.Length <= 0 || piece.Length > maxPieceLength {
		return nil, fmt.Errorf("invalid piece metadata")
	}
	data := make([]byte, piece.Length)
	blocks := make([]blockState, (piece.Length+blockSize-1)/blockSize)
	for index := range blocks {
		blocks[index].length = min(blockSize, piece.Length-index*blockSize)
	}
	w.requestedPieces[uint32(piece.Index)] = true
	deadline := time.Now().Add(w.timeout)
	inFlight, received := 0, 0
	for received < len(data) {
		if !w.choked {
			for index := range blocks {
				block := &blocks[index]
				if inFlight >= requestPipeline {
					break
				}
				if block.received || block.requested {
					continue
				}
				request, _ := peerprotocol.EncodeRequest(peerprotocol.BlockRequest{
					Index: uint32(piece.Index), Begin: uint32(index * blockSize), Length: uint32(block.length),
				})
				if err := w.send(request); err != nil {
					return nil, err
				}
				block.requested, block.sent = true, true
				inFlight++
			}
		}
		message, err := w.receive(deadline)
		if err != nil {
			return nil, err
		}
		wasChoked := w.choked
		block, err := w.handle(message)
		if err != nil {
			return nil, err
		}
		if !wasChoked && w.choked {
			// Peers may discard queued requests when choking us. Reissue only
			// missing blocks after unchoke, accepting any late responses meanwhile.
			for index := range blocks {
				blocks[index].requested = false
			}
			inFlight = 0
		}
		if block == nil {
			continue
		}
		if block.Index != uint32(piece.Index) {
			if w.requestedPieces[block.Index] {
				continue // A late response for a previously requested piece.
			}
			return nil, fmt.Errorf("received unrequested piece %d", block.Index)
		}
		if block.Begin%blockSize != 0 || uint64(block.Begin) >= uint64(len(data)) {
			return nil, fmt.Errorf("invalid block offset %d", block.Begin)
		}
		state := &blocks[int(block.Begin)/blockSize]
		if !state.sent || len(block.Block) != state.length {
			return nil, fmt.Errorf("unexpected block length or unrequested block")
		}
		start := int(block.Begin)
		if state.received {
			if !bytes.Equal(data[start:start+state.length], block.Block) {
				return nil, fmt.Errorf("conflicting duplicate block")
			}
			continue
		}
		copy(data[start:start+state.length], block.Block)
		state.received = true
		if state.requested {
			inFlight--
		}
		received += state.length
		// Only new block bytes extend the deadline. Keep-alives, duplicates,
		// and control messages cannot keep a stalled piece alive forever.
		deadline = time.Now().Add(w.timeout)
	}
	if sha1.Sum(data) != piece.Hash {
		return nil, fmt.Errorf("SHA-1 mismatch for piece %d", piece.Index)
	}
	return data, nil
}
