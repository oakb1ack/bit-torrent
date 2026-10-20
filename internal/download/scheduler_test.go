package download

import (
	"crypto/sha1"
	"strings"
	"sync"
	"testing"
)

func TestPieceMetadata(t *testing.T) {
	// Hashes are raw bytes, including zeros; they are not hexadecimal strings.
	hashes := strings.Repeat("\x00", sha1.Size) + strings.Repeat("\x80", sha1.Size) + strings.Repeat("\xff", sha1.Size)
	scheduler, err := NewScheduler(10, 4, hashes)
	if err != nil {
		t.Fatal(err)
	}
	for index, length := range []int{4, 4, 2} {
		piece, ok := scheduler.Next(func(int) bool { return true })
		if !ok || piece.Index != index || piece.Length != length || string(piece.Hash[:]) != hashes[index*sha1.Size:(index+1)*sha1.Size] {
			t.Fatalf("piece %d = %#v, %v; want length %d and its corresponding hash", index, piece, ok, length)
		}
	}
	if scheduler.Done() {
		t.Fatal("assigned pieces are not yet complete")
	}
}

func TestExactPieceBoundary(t *testing.T) {
	scheduler := newTestScheduler(t, 2)
	for range 2 {
		piece, ok := scheduler.Next(func(int) bool { return true })
		if !ok || piece.Length != 4 {
			t.Fatalf("piece = %#v, %v; an exact final piece must retain its full length", piece, ok)
		}
	}
}

func TestRejectInvalidPieceMetadata(t *testing.T) {
	tests := []struct {
		name        string
		totalLength int64
		pieceLength int
		hashes      string
	}{
		{"negative total", -1, 4, ""},
		{"zero piece length", 4, 0, strings.Repeat("x", 20)},
		{"negative piece length", 4, -1, strings.Repeat("x", 20)},
		{"partial hash", 4, 4, strings.Repeat("x", 19)},
		{"too few hashes", 5, 4, strings.Repeat("x", 20)},
		{"too many hashes", 4, 4, strings.Repeat("x", 40)},
		{"no hashes for nonempty file", 4, 4, ""},
		{"hash for empty file", 0, 4, strings.Repeat("x", 20)},
		// Computing the count with (total + length - 1) would overflow here.
		{"huge total with too few hashes", 1<<63 - 1, 4, strings.Repeat("x", 20)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if scheduler, err := NewScheduler(tt.totalLength, tt.pieceLength, tt.hashes); scheduler != nil || err == nil {
				t.Fatalf("NewScheduler = %v, %v; want error", scheduler, err)
			}
		})
	}
}

func TestSchedulerSelectsOnlyAvailablePieces(t *testing.T) {
	scheduler := newTestScheduler(t, 3)
	hasOne := func(index int) bool { return index == 1 }
	piece, ok := scheduler.Next(hasOne)
	if !ok || piece.Index != 1 {
		t.Fatalf("assigned %#v, %v; want piece 1", piece, ok)
	}
	if _, ok := scheduler.Next(hasOne); ok {
		t.Fatal("assigned an unavailable or already assigned piece")
	}
	if scheduler.Done() {
		t.Fatal("no eligible work for one peer does not mean the download is done")
	}
	piece, ok = scheduler.Next(func(index int) bool { return index == 0 })
	if !ok || piece.Index != 0 {
		t.Fatalf("other peer got %#v, %v; want piece 0", piece, ok)
	}
	if completed, total := scheduler.Progress(); completed != 0 || total != 3 {
		t.Fatalf("progress = %d/%d; assigned pieces must not count as complete", completed, total)
	}
}

func TestSchedulerRetryAndComplete(t *testing.T) {
	scheduler := newTestScheduler(t, 2)
	all := func(int) bool { return true }
	first, _ := scheduler.Next(all)
	second, _ := scheduler.Next(all)
	if _, ok := scheduler.Next(all); ok || scheduler.Done() {
		t.Fatal("all pieces are assigned, but none are complete")
	}
	if err := scheduler.Retry(first.Index); err != nil {
		t.Fatal(err)
	}
	retried, ok := scheduler.Next(all)
	if !ok || retried != first {
		t.Fatalf("retried piece = %#v, %v; want %#v", retried, ok, first)
	}
	if err := scheduler.Complete(retried.Index); err != nil {
		t.Fatal(err)
	}
	if scheduler.Done() {
		t.Fatal("the second piece has not completed yet")
	}
	if err := scheduler.Complete(second.Index); err != nil {
		t.Fatal(err)
	}
	if !scheduler.Done() {
		t.Fatal("all pieces completed, but Done returned false")
	}
	if completed, total := scheduler.Progress(); completed != 2 || total != 2 {
		t.Fatalf("progress = %d/%d; want 2/2", completed, total)
	}
	if _, ok := scheduler.Next(all); ok {
		t.Fatal("completed piece was assigned again")
	}
	if err := scheduler.Complete(first.Index); err == nil {
		t.Fatal("duplicate completion was accepted")
	}
	if err := scheduler.Retry(first.Index); err == nil {
		t.Fatal("completed piece was returned to pending")
	}
}

func TestRetryDoesNotStarveOtherPieces(t *testing.T) {
	scheduler := newTestScheduler(t, 3)
	first, _ := scheduler.Next(func(int) bool { return true })
	if err := scheduler.Retry(first.Index); err != nil {
		t.Fatal(err)
	}
	next, ok := scheduler.Next(func(int) bool { return true })
	if !ok || next.Index != 1 {
		t.Fatalf("next piece = %#v, %v; a failed piece should not monopolize the scheduler", next, ok)
	}
}

func TestSchedulerRejectsInvalidTransitions(t *testing.T) {
	scheduler := newTestScheduler(t, 2)
	for _, index := range []int{-1, 0, 2} {
		if err := scheduler.Retry(index); err == nil {
			t.Errorf("Retry(%d) accepted an unassigned or invalid piece", index)
		}
		if err := scheduler.Complete(index); err == nil {
			t.Errorf("Complete(%d) accepted an unassigned or invalid piece", index)
		}
	}
	if _, ok := scheduler.Next(nil); ok {
		t.Fatal("nil availability callback claimed work")
	}
	if completed, total := scheduler.Progress(); completed != 0 || total != 2 {
		t.Fatalf("failed transitions changed progress to %d/%d", completed, total)
	}
}

func TestClaimReturnsIndependentMetadata(t *testing.T) {
	scheduler := newTestScheduler(t, 1)
	piece, _ := scheduler.Next(func(int) bool { return true })
	original := piece
	piece.Hash[0] ^= 0xff
	if err := scheduler.Retry(original.Index); err != nil {
		t.Fatal(err)
	}
	retried, _ := scheduler.Next(func(int) bool { return true })
	if retried != original {
		t.Fatal("modifying a claimed piece changed the scheduler's metadata")
	}
}

func TestEmptyDownloadIsComplete(t *testing.T) {
	scheduler, err := NewScheduler(0, 4, "")
	if err != nil {
		t.Fatal(err)
	}
	if !scheduler.Done() {
		t.Fatal("empty download should already be complete")
	}
	if _, ok := scheduler.Next(func(int) bool { return true }); ok {
		t.Fatal("empty download produced a piece")
	}
	if completed, total := scheduler.Progress(); completed != 0 || total != 0 {
		t.Fatalf("empty progress = %d/%d", completed, total)
	}
}

func TestConcurrentWorkersDoNotClaimDuplicatePieces(t *testing.T) {
	const count = 100
	scheduler := newTestScheduler(t, count)
	claims := make(chan int, count)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			<-start
			for {
				piece, ok := scheduler.Next(func(int) bool { return true })
				if !ok {
					return
				}
				claims <- piece.Index
				if err := scheduler.Complete(piece.Index); err != nil {
					t.Errorf("complete piece %d: %v", piece.Index, err)
				}
				// Exercise reads while other workers change the scheduler.
				scheduler.Progress()
				scheduler.Done()
			}
		})
	}
	close(start)
	seen := make(map[int]int)
	go func() { workers.Wait(); close(claims) }()
	for index := range claims {
		seen[index]++
	}
	for index := range count {
		if seen[index] != 1 {
			t.Errorf("piece %d claimed %d times; want once", index, seen[index])
		}
	}
	if !scheduler.Done() {
		t.Fatal("workers exited without completing all pieces")
	}
}

func newTestScheduler(t *testing.T, count int) *Scheduler {
	t.Helper()
	scheduler, err := NewScheduler(int64(count*4), 4, strings.Repeat("x", count*sha1.Size))
	if err != nil {
		t.Fatal(err)
	}
	return scheduler
}
