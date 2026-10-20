package download

import (
	"fmt"
	"sync"
)

type pieceState byte

const (
	pending pieceState = iota
	assigned
	complete
)

// Scheduler assigns pieces to workers. Its mutex protects both the search for
// work and the state change, so two workers cannot claim the same pending piece.
// Create it with NewScheduler and share its pointer; do not copy it.
type Scheduler struct {
	mu        sync.Mutex
	pieces    []Piece
	states    []pieceState
	next      int
	completed int
	changed   chan struct{}
}

func NewScheduler(totalLength int64, pieceLength int, hashes string) (*Scheduler, error) {
	pieces, err := parsePieces(totalLength, pieceLength, hashes)
	if err != nil {
		return nil, err
	}
	return &Scheduler{
		pieces: pieces,
		// pending is zero, so every piece starts pending.
		states:  make([]pieceState, len(pieces)),
		changed: make(chan struct{}),
	}, nil
}

// Next claims a pending piece that this peer has. Pass the peer's HasPiece method
// as hasPiece. The callback must be quick, perform no I/O, and must not call back
// into the scheduler because it runs while the mutex is held.
//
// A false result means no eligible piece is pending right now. Other peers may
// have work, or pieces may still be assigned; use Done to check global completion.
func (s *Scheduler) Next(hasPiece func(int) bool) (Piece, bool) {
	if hasPiece == nil {
		return Piece{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for range len(s.pieces) {
		index := s.next
		// Rotate the starting point so a repeatedly retried piece does not
		// prevent workers from trying other pending pieces.
		s.next = (s.next + 1) % len(s.pieces)
		if s.states[index] == pending && hasPiece(index) {
			s.states[index] = assigned
			return s.pieces[index], true
		}
	}
	return Piece{}, false
}

// Retry returns an assigned piece to pending after a failed attempt.
// The worker must stop working on it before calling Retry.
func (s *Scheduler) Retry(index int) error {
	return s.finish(index, pending)
}

// Complete records success. Call it only after the entire piece has been
// downloaded and its hash verified; verification belongs to the download worker.
func (s *Scheduler) Complete(index int) error {
	return s.finish(index, complete)
}

func (s *Scheduler) finish(index int, state pieceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.pieces) {
		return fmt.Errorf("piece index %d is out of range", index)
	}
	if s.states[index] != assigned {
		return fmt.Errorf("piece %d is not assigned to a worker", index)
	}
	s.states[index] = state
	if state == complete {
		s.completed++
	}
	close(s.changed)
	s.changed = make(chan struct{})
	return nil
}

// Changes returns a notification channel that closes when work is retried or
// completed. Take this snapshot before Next so a change cannot be missed between
// discovering no work and waiting. Notifications wake all waiting workers.
func (s *Scheduler) Changes() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.changed
}

func (s *Scheduler) hasAssigned() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, state := range s.states {
		if state == assigned {
			return true
		}
	}
	return false
}

// Progress returns completed and total piece counts in a consistent snapshot.
func (s *Scheduler) Progress() (completed, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.completed, len(s.pieces)
}

func (s *Scheduler) Done() bool {
	completed, total := s.Progress()
	return completed == total
}
