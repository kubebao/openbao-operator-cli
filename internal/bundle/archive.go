package bundle

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	maxArchiveBytes  = 64 << 20
	maxExpandedBytes = 96 << 20
)

var entryNames = []string{"manifest.json", "summary.txt", "findings.json", "evidence/capture.json"}

func entryLimit(name string) int64 {
	switch name {
	case "manifest.json":
		return 256 << 10
	case "summary.txt":
		return 2 << 20
	case "findings.json":
		return 8 << 20
	case "evidence/capture.json":
		return 64 << 20
	default:
		return 0
	}
}

// CheckOutput catches an existing destination before cluster access. Write also
// enforces no replacement atomically, including files created during collection.
func CheckOutput(path string, overwrite bool) error {
	if path == "" {
		return errors.New("--output-file is required")
	}
	info, err := os.Lstat(path)
	if err == nil {
		if !overwrite {
			return errors.New("output file exists; use --overwrite to replace it")
		}
		if !info.Mode().IsRegular() {
			return errors.New("output path must be a regular file")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return errors.New("cannot inspect output path")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return errors.New("output directory does not exist or is not accessible")
	}
	return nil
}

// Write finalizes a private archive in the destination directory, then publishes
// it atomically. A failed write never exposes a partially finalized destination.
func Write(ctx context.Context, path string, b Bundle, overwrite bool) error {
	if err := CheckOutput(path, overwrite); err != nil {
		return err
	}
	entries, err := b.entries()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".kubebao-bundle-*")
	if err != nil {
		return errors.New("cannot create private bundle file")
	}
	defer func() { _ = os.Remove(file.Name()) }()
	defer func() { _ = file.Close() }()
	if err := writeArchive(ctx, file, entries); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || info.Size() > maxArchiveBytes {
		return errors.New("compressed bundle exceeds 64 MiB or cannot be inspected")
	}
	if err := file.Sync(); err != nil {
		return errors.New("cannot sync bundle file")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot finalize bundle file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if overwrite {
		err = os.Rename(file.Name(), path)
	} else {
		// A same-directory hard link publishes an already finalized inode and
		// fails if any destination, including a symlink, now exists.
		err = os.Link(file.Name(), path)
	}
	if err != nil {
		return errors.New("cannot publish bundle; destination may exist or be inaccessible")
	}
	return nil
}

func writeArchive(ctx context.Context, destination io.Writer, entries map[string][]byte) error {
	zip := gzip.NewWriter(destination)
	archive := tar.NewWriter(zip)
	for _, name := range entryNames {
		if err := ctx.Err(); err != nil {
			return err
		}
		data := entries[name]
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			return errors.New("cannot write bundle header")
		}
		if _, err := archive.Write(data); err != nil {
			return errors.New("cannot write bundle entry")
		}
	}
	if err := archive.Close(); err != nil {
		return errors.New("cannot finalize bundle archive")
	}
	if err := zip.Close(); err != nil {
		return errors.New("cannot finalize bundle compression")
	}
	return nil
}

// Read checks compressed and expanded limits, checksums, entry names and types,
// duplicate entries, and typed evidence. It never extracts archive members.
func Read(ctx context.Context, path string) (Bundle, error) {
	if err := ctx.Err(); err != nil {
		return Bundle{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Bundle{}, errors.New("cannot open support bundle")
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(filepath.Base(path))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return Bundle{}, errors.New("bundle must be a regular file of at most 64 MiB")
	}
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return Bundle{}, errors.New("cannot open support bundle")
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxArchiveBytes {
		return Bundle{}, errors.New("bundle must be a regular file of at most 64 MiB")
	}
	limited := &io.LimitedReader{R: file, N: maxArchiveBytes + 1}
	b, err := readArchive(ctx, limited)
	if limited.N == 0 {
		return Bundle{}, errors.New("compressed bundle exceeds 64 MiB")
	}
	return b, err
}

func readArchive(ctx context.Context, source io.Reader) (Bundle, error) {
	compressed := bufio.NewReader(source)
	zip, err := gzip.NewReader(compressed)
	if err != nil {
		return Bundle{}, errors.New("invalid gzip bundle")
	}
	defer func() { _ = zip.Close() }()
	zip.Multistream(false)
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx, zip}, maxExpandedBytes+1))
	if err != nil || len(data) > maxExpandedBytes {
		return Bundle{}, errors.New("bundle decompression failed or exceeded 96 MiB")
	}
	if _, err := compressed.Peek(1); !errors.Is(err, io.EOF) {
		return Bundle{}, errors.New("trailing data or multiple gzip streams are unsupported")
	}
	entries, err := readEntries(ctx, data)
	if err != nil {
		return Bundle{}, err
	}
	var b Bundle
	if err := decodeJSON(entries["manifest.json"], &b.Manifest); err != nil {
		return Bundle{}, fmt.Errorf("manifest: %w", err)
	}
	if err := decodeJSON(entries["evidence/capture.json"], &b.Capture); err != nil {
		return Bundle{}, fmt.Errorf("capture: %w", err)
	}
	if err := b.Validate(); err != nil {
		return Bundle{}, err
	}
	return b, nil
}

func readEntries(ctx context.Context, data []byte) (map[string][]byte, error) {
	source := bytes.NewReader(data)
	archive := tar.NewReader(source)
	entries := map[string][]byte{}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.New("invalid tar bundle")
		}
		limit := entryLimit(header.Name)
		if limit == 0 || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > limit ||
			header.Linkname != "" || len(header.PAXRecords) > 0 || entries[header.Name] != nil {
			return nil, errors.New("unsafe, duplicate, oversized, or unsupported bundle entry")
		}
		body, err := io.ReadAll(archive)
		if err != nil {
			return nil, errors.New("cannot read bundle entry")
		}
		entries[header.Name] = body
	}
	if len(entries) != len(entryNames) {
		return nil, errors.New("required bundle entries are missing")
	}
	// tar permits zero padding after its end marker. Reject hidden trailing data.
	for source.Len() > 0 {
		value, _ := source.ReadByte()
		if value != 0 {
			return nil, errors.New("unexpected data after tar end marker")
		}
	}
	return entries, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
