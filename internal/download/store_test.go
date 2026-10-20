package download

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreAssemblesOutOfOrderPieces(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Layout{Name: "sample.bin", Length: 10, PieceLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Finish(); err == nil {
		t.Fatal("published incomplete file")
	}
	for _, p := range []struct {
		index int
		data  string
	}{{2, "ij"}, {0, "abcd"}, {1, "efgh"}} {
		if err := s.WritePiece(Piece{Index: p.index, Length: len(p.data)}, []byte(p.data)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "sample.bin")); !os.IsNotExist(err) {
		t.Fatal("output published before verification finished")
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sample.bin"))
	if err != nil || string(data) != "abcdefghij" {
		t.Fatalf("output = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sample.bin.part")); !os.IsNotExist(err) {
		t.Fatal("temporary output remains after finish")
	}
}

func TestStoreSplitsPiecesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Layout{Name: "bundle", Length: 10, PieceLength: 4, Files: []File{
		{Path: []string{"empty"}, Length: 0}, {Path: []string{"a.txt"}, Length: 3}, {Path: []string{"nested", "b.txt"}, Length: 5}, {Path: []string{"c.txt"}, Length: 2},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for index, data := range []string{"abcd", "efgh", "ij"} {
		if err := s.WritePiece(Piece{Index: index, Length: len(data)}, []byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"empty": "", "a.txt": "abc", filepath.Join("nested", "b.txt"): "defgh", "c.txt": "ij"} {
		data, err := os.ReadFile(filepath.Join(dir, "bundle", path))
		if err != nil || string(data) != want {
			t.Fatalf("%s = %q, %v; want %q", path, data, err, want)
		}
	}
}

func TestStoreRejectsUnsafeAndConflictingLayouts(t *testing.T) {
	tests := []Layout{
		{Name: "../outside", Length: 1, PieceLength: 1},
		{Name: "a/b", Length: 1, PieceLength: 1},
		{Name: "a\\b", Length: 1, PieceLength: 1},
		{Name: "x:", Length: 1, PieceLength: 1},
		{Name: "x.", Length: 1, PieceLength: 1},
		{Name: "x", Length: 1, PieceLength: 0},
		{Name: "x", Length: 1, PieceLength: maxPieceLength + 1},
		{Name: "x", Length: 1, PieceLength: 1, Files: []File{{Path: []string{"..", "outside"}, Length: 1}}},
		{Name: "x", Length: 1, PieceLength: 1, Files: []File{{Path: []string{"nested", "/absolute"}, Length: 1}}},
		{Name: "x", Length: 1, PieceLength: 1, Files: []File{{Path: []string{"a"}, Length: 2}}},
		{Name: "x", Length: 1, PieceLength: 1, Files: []File{{Path: []string{"a"}, Length: 0}}},
		{Name: "x", Length: 2, PieceLength: 1, Files: []File{{Path: []string{"a"}, Length: 1}, {Path: []string{"A"}, Length: 1}}},
		{Name: "x", Length: 2, PieceLength: 1, Files: []File{{Path: []string{"a"}, Length: 1}, {Path: []string{"a", "b"}, Length: 1}}},
	}
	for _, layout := range tests {
		t.Run(layout.Name, func(t *testing.T) {
			if s, err := NewStore(t.TempDir(), layout); err == nil {
				s.Close()
				t.Fatal("accepted invalid layout")
			}
		})
	}
}

func TestStoreDoesNotOverwriteExistingFiles(t *testing.T) {
	for _, name := range []string{"file", "file.part"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, name)
			os.WriteFile(path, []byte("keep me"), 0600)
			if s, err := NewStore(dir, Layout{Name: "file", Length: 1, PieceLength: 1}); err == nil {
				s.Close()
				t.Fatal("overwrote existing output")
			}
			got, _ := os.ReadFile(path)
			if string(got) != "keep me" {
				t.Fatal("existing file changed")
			}
		})
	}
}

func TestStoreRefusesDestinationCreatedDuringDownload(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Layout{Name: "file", Length: 1, PieceLength: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.WritePiece(Piece{Length: 1}, []byte("x")); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "file"), []byte("keep me"), 0600)
	if err := s.Finish(); err == nil {
		t.Fatal("replaced existing destination")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "file"))
	if string(got) != "keep me" {
		t.Fatal("destination changed")
	}
}

func TestStorePreservesPartialAndRejectsInvalidWrites(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Layout{Name: "file", Length: 5, PieceLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []Piece{{Index: -1, Length: 4}, {Index: 2, Length: 4}, {Index: 0, Length: 3}, {Index: 1, Length: 4}} {
		if err := s.WritePiece(p, bytes.Repeat([]byte{'x'}, p.Length)); err == nil {
			t.Fatal("accepted invalid piece")
		}
	}
	if err := s.WritePiece(Piece{Index: 0, Length: 4}, []byte("abcd")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "file.part")); err != nil {
		t.Fatal(err)
	}
	if err := s.WritePiece(Piece{Length: 4}, []byte("abcd")); err == nil {
		t.Fatal("wrote to closed store")
	}
}

func TestStoreFinishesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir, Layout{Name: "empty", PieceLength: 4})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Finish(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "empty"))
	if err != nil || info.Size() != 0 {
		t.Fatalf("empty file = %v, %v", info, err)
	}
}
