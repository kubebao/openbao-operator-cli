package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"

	shared "github.com/kubebao/openbao-operator-client/evidence"
	"github.com/kubebao/openbao-operator-client/operations"
)

const (
	requestVersion  = "kubebao-control-request/v1"
	maxRequestBytes = 32 * 1024
)

// Request preserves intent before submission. It is neither a receipt nor executable configuration.
type Request struct {
	FormatVersion string                  `json:"formatVersion"`
	Control       *operations.Plan        `json:"control,omitempty"`
	Restart       *operations.RestartPlan `json:"restart,omitempty"`
}

func (r Request) Validate() error {
	if r.FormatVersion != requestVersion || (r.Control == nil) == (r.Restart == nil) {
		return errors.New("unsupported or invalid saved request")
	}
	if r.Control != nil {
		return r.Control.Validate()
	}
	return r.Restart.Validate()
}

func (r Request) Target() shared.Reference {
	if r.Control != nil {
		return r.Control.Target
	}
	return r.Restart.Target
}

// CheckRequestOutput rejects existing destinations, including symlinks. WriteRequest checks again when publishing.
func CheckRequestOutput(path string) error {
	if path == "" {
		return errors.New("request file path is empty")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("request file already exists or cannot be inspected; choose a new path")
	}
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || !info.IsDir() {
		return errors.New("request file directory does not exist or is inaccessible")
	}
	return nil
}

// WriteRequest publishes a complete mode-0600 file before any mutation. It never overwrites a destination.
func WriteRequest(ctx context.Context, path string, request Request) error {
	if err := request.Validate(); err != nil {
		return errors.New("cannot save invalid request")
	}
	if err := CheckRequestOutput(path); err != nil {
		return err
	}
	data, err := json.Marshal(request)
	if err != nil || len(data)+1 > maxRequestBytes {
		return errors.New("cannot encode bounded request")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".kubebao-request-*")
	if err != nil {
		return errors.New("cannot create private request file")
	}
	defer func() { _ = os.Remove(file.Name()) }()
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return errors.New("cannot write request file")
	}
	if err := file.Sync(); err != nil {
		return errors.New("cannot sync request file")
	}
	if err := file.Close(); err != nil {
		return errors.New("cannot finalize request file")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), path); err != nil {
		return errors.New("cannot publish request file; destination may exist or be inaccessible")
	}
	return nil
}

// ReadRequest bounds input, rejects links and special files, and accepts only the saved request schema.
func ReadRequest(ctx context.Context, path string) (Request, error) {
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Request{}, errors.New("cannot open request file directory")
	}
	defer func() { _ = root.Close() }()
	info, err := root.Lstat(filepath.Base(path))
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRequestBytes {
		return Request{}, errors.New("request must be a regular file of at most 32 KiB")
	}
	// Nonblocking, no-follow open prevents a replacement FIFO or link from changing the file type after inspection.
	file, err := root.OpenFile(filepath.Base(path), os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Request{}, errors.New("cannot open regular request file")
	}
	defer func() { _ = file.Close() }()
	info, err = file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxRequestBytes {
		return Request{}, errors.New("request must be a regular file of at most 32 KiB")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRequestBytes+1))
	if err != nil || len(data) > maxRequestBytes {
		return Request{}, errors.New("cannot read bounded request file")
	}
	if err := ctx.Err(); err != nil {
		return Request{}, err
	}
	return decodeRequest(data)
}

func decodeRequest(data []byte) (Request, error) {
	invalid := errors.New("invalid saved request format; use a file created with --request-file")
	if len(data) > maxRequestBytes || !utf8.Valid(data) {
		return Request{}, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return Request{}, invalid
	}
	request, seen := Request{}, map[string]bool{}
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return Request{}, invalid
		}
		seen[name] = true
		switch name {
		case "formatVersion":
			err = decoder.Decode(&request.FormatVersion)
		case "control":
			err = decoder.Decode(&request.Control)
			if request.Control == nil {
				return Request{}, invalid
			}
		case "restart":
			err = decoder.Decode(&request.Restart)
			if request.Restart == nil {
				return Request{}, invalid
			}
		default:
			return Request{}, invalid
		}
		if err != nil {
			return Request{}, invalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || request.Validate() != nil {
		return Request{}, invalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Request{}, invalid
	}
	return request, nil
}
