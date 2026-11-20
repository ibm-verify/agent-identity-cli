// Package input reads the data given to commands with -d, parses the login
// payload and prints its template. See spec.md.
package input

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// ErrInvalidInput means the data is malformed or incomplete.
var ErrInvalidInput = errors.New("invalid input")

// MaxSize is the largest input accepted, in bytes.
const MaxSize = 1 << 20

// Data is the input read for -d.
type Data struct {
	Bytes []byte
	// BaseDir resolves relative @ paths. Empty means the working directory.
	BaseDir string
}

// Read returns the data in the file at path, or from stdin when path is "-".
func Read(path string, stdin *os.File) (*Data, error) {
	var (
		r       io.Reader
		baseDir string
		source  = "stdin"
	)
	if path == "-" {
		info, err := stdin.Stat()
		if err != nil {
			return nil, unreadable(source, err)
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return nil, i18n.Wrap(ErrInvalidInput, "input.terminal_stdin", nil)
		}
		r = stdin
	} else {
		ref, err := profile.Source("@" + path).Resolve("")
		if err != nil {
			return nil, err
		}
		source = ref.Path()
		f, err := os.Open(source)
		if err != nil {
			return nil, unreadable(source, err)
		}
		defer f.Close()
		r, baseDir = f, filepath.Dir(source)
	}

	b, err := io.ReadAll(io.LimitReader(r, MaxSize+1))
	if err != nil {
		return nil, unreadable(source, err)
	}
	if len(b) > MaxSize {
		return nil, i18n.Wrap(ErrInvalidInput, "input.too_large", map[string]any{"Max": MaxSize})
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil, i18n.Wrap(ErrInvalidInput, "input.empty", nil)
	}
	slog.Debug("input read", "source", source, "bytes", len(b))
	return &Data{Bytes: b, BaseDir: baseDir}, nil
}

func unreadable(path string, err error) error {
	return i18n.Wrap(profile.ErrSourceUnavailable, "source.unreadable", map[string]any{"Path": path, "Error": err.Error()})
}

// decode reads JSON when the data starts with "{", and YAML otherwise. Unknown
// properties are ignored. The decoder's error is dropped because it can quote
// a value from the data.
func decode(b []byte, out any) error {
	var err error
	if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
		err = json.Unmarshal(b, out)
	} else {
		err = yaml.Unmarshal(b, out)
	}
	if err != nil {
		return i18n.Wrap(ErrInvalidInput, "input.invalid_data", nil)
	}
	return nil
}
