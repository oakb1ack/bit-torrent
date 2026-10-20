package download

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type File struct {
	Path   []string
	Length int64
}

// Layout maps the torrent's logical byte stream to a file or directory.
// Files is nil for single-file torrents; multi-file paths are relative to Name.
type Layout struct {
	Name        string
	Length      int64
	PieceLength int
	Files       []File
}

type storedFile struct {
	file       *os.File
	start, end int64
}

// Store receives pieces serially from the coordinator. Incomplete output has a
// .part suffix; Finish publishes it only after every piece has been written.
type Store struct {
	root      *os.Root
	layout    Layout
	files     []storedFile
	written   []bool
	temporary string
	finished  bool
}

func safeComponent(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.IsLocal(name) && !strings.ContainsAny(name, "/\\:\x00") &&
		!strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ")
}

func (layout Layout) Validate() error {
	if !safeComponent(layout.Name) {
		return fmt.Errorf("unsafe torrent name %q", layout.Name)
	}
	if layout.Length < 0 || layout.PieceLength <= 0 || layout.PieceLength > maxPieceLength {
		return fmt.Errorf("invalid file size or piece length (maximum %d bytes)", maxPieceLength)
	}
	if layout.Files != nil {
		var total int64
		seen := make(map[string]bool)
		for _, file := range layout.Files {
			if len(file.Path) == 0 || file.Length < 0 || file.Length > layout.Length-total {
				return fmt.Errorf("invalid file path or length")
			}
			for _, part := range file.Path {
				if !safeComponent(part) {
					return fmt.Errorf("unsafe file path component %q", part)
				}
			}
			path := strings.ToLower(filepath.Join(file.Path...))
			if seen[path] {
				return fmt.Errorf("duplicate file path %q", path)
			}
			seen[path] = true
			total += file.Length
		}
		if total != layout.Length {
			return fmt.Errorf("file lengths do not match torrent length")
		}
	}
	return nil
}

func NewStore(directory string, layout Layout) (_ *Store, err error) {
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, layout: layout, temporary: layout.Name + ".part"}
	created := false
	defer func() {
		if err != nil {
			s.closeFiles()
			if created {
				root.RemoveAll(s.temporary)
			}
			root.Close()
		}
	}()
	if _, err := root.Lstat(layout.Name); err == nil {
		return nil, fmt.Errorf("output %q already exists", layout.Name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	count := layout.Length / int64(layout.PieceLength)
	if layout.Length%int64(layout.PieceLength) != 0 {
		count++
	}
	if count > int64(int(^uint(0)>>1)) {
		return nil, fmt.Errorf("too many pieces")
	}
	s.written = make([]bool, int(count))
	if layout.Files == nil {
		file, err := root.OpenFile(s.temporary, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
		if err != nil {
			return nil, err
		}
		created = true
		s.files = append(s.files, storedFile{file: file, end: layout.Length})
		if err := file.Truncate(layout.Length); err != nil {
			return nil, err
		}
	} else {
		if err := root.Mkdir(s.temporary, 0755); err != nil {
			return nil, err
		}
		created = true
		var offset int64
		for _, entry := range layout.Files {
			path := filepath.Join(append([]string{s.temporary}, entry.Path...)...)
			if err := root.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return nil, err
			}
			file, err := root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
			if err != nil {
				return nil, err
			}
			s.files = append(s.files, storedFile{file: file, start: offset, end: offset + entry.Length})
			if err := file.Truncate(entry.Length); err != nil {
				return nil, err
			}
			offset += entry.Length
		}
	}
	return s, nil
}

func (s *Store) WritePiece(piece Piece, data []byte) error {
	if s.finished || s.root == nil {
		return fmt.Errorf("piece store is closed")
	}
	if piece.Index < 0 || piece.Index >= len(s.written) {
		return fmt.Errorf("invalid piece index %d", piece.Index)
	}
	offset := int64(piece.Index) * int64(s.layout.PieceLength)
	expected := int(min(int64(s.layout.PieceLength), s.layout.Length-offset))
	if len(data) != expected || piece.Length != expected {
		return fmt.Errorf("piece %d length does not match file layout", piece.Index)
	}
	remaining := data
	for _, entry := range s.files {
		if len(remaining) == 0 {
			break
		}
		if offset >= entry.end {
			continue
		}
		if entry.file == nil {
			return fmt.Errorf("piece store file is closed")
		}
		length := int(min(int64(len(remaining)), entry.end-offset))
		n, err := entry.file.WriteAt(remaining[:length], offset-entry.start)
		if err != nil {
			return err
		}
		if n != length {
			return io.ErrShortWrite
		}
		remaining = remaining[length:]
		offset += int64(length)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("piece extends beyond output files")
	}
	s.written[piece.Index] = true
	return nil
}

func (s *Store) Finish() error {
	if s.finished || s.root == nil {
		return fmt.Errorf("piece store is closed")
	}
	for _, written := range s.written {
		if !written {
			return fmt.Errorf("cannot finish an incomplete download")
		}
	}
	for _, entry := range s.files {
		if entry.file != nil {
			if err := entry.file.Sync(); err != nil {
				return err
			}
		}
	}
	if err := s.closeFiles(); err != nil {
		return err
	}
	if s.layout.Files == nil {
		// Unlike Rename, Link refuses to replace a destination created while
		// downloading. The temporary and final file are on the same filesystem.
		if err := s.root.Link(s.temporary, s.layout.Name); err != nil {
			return err
		}
		if err := s.root.Remove(s.temporary); err != nil {
			return err
		}
	} else {
		if _, err := s.root.Lstat(s.layout.Name); err == nil {
			return fmt.Errorf("output already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := s.root.Rename(s.temporary, s.layout.Name); err != nil {
			return err
		}
	}
	s.finished = true
	return nil
}

func (s *Store) closeFiles() error {
	var result error
	for i := range s.files {
		if s.files[i].file != nil {
			result = errors.Join(result, s.files[i].file.Close())
			s.files[i].file = nil
		}
	}
	return result
}

// Close preserves incomplete .part output for inspection. Resume is not enabled.
func (s *Store) Close() error {
	err := s.closeFiles()
	if s.root != nil {
		err = errors.Join(err, s.root.Close())
		s.root = nil
	}
	return err
}
