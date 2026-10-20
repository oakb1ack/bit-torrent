package download

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oakb1ack/bit-torrent/internal/peer"
)

type Options struct {
	// Timeout limits how long a peer may go without delivering useful data.
	// A zero value selects 30 seconds.
	Timeout time.Duration
	// OnPiece runs serially after a verified piece is stored and marked complete.
	OnPiece func(Piece) error
}

type pieceResult struct {
	piece Piece
	data  []byte
	err   error
	ack   chan struct{}
}

// Run downloads all scheduled pieces. It owns and closes the supplied peer
// connections. store is called serially, only with hash-verified data. Completion
// is recorded only after storage succeeds; storage errors stop all workers.
func Run(ctx context.Context, scheduler *Scheduler, connections []*peer.Connection, store func(Piece, []byte) error, options Options) error {
	defer func() {
		for _, connection := range connections {
			if connection != nil && connection.Conn != nil {
				connection.Conn.Close()
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	if scheduler.Done() {
		return nil
	}
	if store == nil {
		return fmt.Errorf("piece store is required")
	}
	if len(connections) == 0 {
		return fmt.Errorf("no connected peers available for download")
	}
	for _, connection := range connections {
		if connection == nil || connection.Conn == nil {
			return fmt.Errorf("invalid peer connection")
		}
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	if options.Timeout < 0 {
		return fmt.Errorf("peer timeout must be positive")
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	results := make(chan pieceResult)
	_, pieceCount := scheduler.Progress()
	for _, connection := range connections {
		workers.Go(func() {
			err := runWorker(ctx, scheduler, connection, pieceCount, options.Timeout, results)
			if err != nil {
				select {
				case results <- pieceResult{err: err}:
				case <-ctx.Done():
				}
			}
		})
	}
	go func() { workers.Wait(); close(results) }()
	var lastError error
	failed := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-results:
			if !ok {
				completed, total := scheduler.Progress()
				if completed == total {
					return nil
				}
				return fmt.Errorf("download incomplete: %d of %d pieces complete; %d peers failed: %w", completed, total, failed, lastError)
			}
			if result.err != nil {
				lastError = result.err
				failed++
				continue
			}
			if err := store(result.piece, result.data); err != nil {
				return fmt.Errorf("store piece %d: %w", result.piece.Index, err)
			}
			if err := scheduler.Complete(result.piece.Index); err != nil {
				return err
			}
			if options.OnPiece != nil {
				if err := options.OnPiece(result.piece); err != nil {
					return err
				}
			}
			close(result.ack)
			if scheduler.Done() {
				return nil
			}
		}
	}
}

func runWorker(ctx context.Context, scheduler *Scheduler, connection *peer.Connection, pieceCount int, timeout time.Duration, results chan<- pieceResult) error {
	w, err := newWorker(ctx, connection, pieceCount, timeout)
	if err != nil {
		return err
	}
	defer w.close()
	active := -1
	defer func() {
		if active >= 0 {
			scheduler.Retry(active)
		}
	}()
	deadline := time.Now().Add(timeout)
	for !scheduler.Done() {
		changed := scheduler.Changes()
		piece, ok := scheduler.Next(connection.HasPiece)
		if ok {
			active = piece.Index
			data, err := w.downloadPiece(piece)
			if err != nil {
				return fmt.Errorf("piece %d: %w", piece.Index, err)
			}
			ack := make(chan struct{})
			select {
			case results <- pieceResult{piece: piece, data: data, ack: ack}:
			case <-ctx.Done():
				return ctx.Err()
			}
			select {
			case <-ack:
			case <-ctx.Done():
				return ctx.Err()
			}
			active = -1
			deadline = time.Now().Add(timeout)
			continue
		}
		// Wait for peer availability updates or returned work, rather than spin.
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
			// A healthy peer must remain available to take work back from an
			// assigned peer that is still finishing or reaching its own timeout.
			if scheduler.hasAssigned() {
				deadline = time.Now().Add(timeout)
				continue
			}
			return fmt.Errorf("peer has no useful work before timeout")
		case <-changed:
			timer.Stop()
			deadline = time.Now().Add(timeout)
		case incoming := <-w.messages:
			timer.Stop()
			if incoming.err != nil {
				return incoming.err
			}
			block, err := w.handle(incoming.message)
			if err != nil {
				return err
			}
			if block != nil && !w.requestedPieces[block.Index] {
				return fmt.Errorf("unsolicited piece %d", block.Index)
			}
		}
	}
	return nil
}
