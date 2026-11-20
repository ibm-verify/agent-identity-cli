package profile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

// ErrSourceUnavailable means a file reference cannot be used: the path is
// empty, or the file is missing, unreadable or not a regular file.
var ErrSourceUnavailable = errors.New("source unavailable")

// filePrefix marks a value that names a file. It is the only prefix today; a
// new one is another case in the methods below.
const filePrefix = "@"

// Source is a string that is either the value itself or a reference to a file
// that holds it. A value that starts with "@" names a file, and anything else
// is the value.
//
// Only properties declared as Source or SecretSource accept a file reference,
// so the types are the list of them. Everywhere else "@" is an ordinary
// character.
//
// The CLI never copies a referenced file into its state directory. A saved
// reference is an absolute path, and the file is read when it is needed.
//
// Use Source for values that are not secret, such as a CA certificate, and
// SecretSource for secrets.
type Source string

// IsFile reports whether s names a file.
func (s Source) IsFile() bool { return strings.HasPrefix(string(s), filePrefix) }

// Path returns the path of a file reference, and "" for an inline value.
func (s Source) Path() string {
	if !s.IsFile() {
		return ""
	}
	return string(s)[len(filePrefix):]
}

// Resolve returns s with a file reference made absolute and checked. A
// relative path is taken from baseDir, or from the working directory when
// baseDir is empty. The file must exist and be a regular file. Its contents are
// not read. An inline value is returned unchanged.
func (s Source) Resolve(baseDir string) (Source, error) {
	if !s.IsFile() {
		return s, nil
	}

	path := s.Path()
	if path == "" {
		return "", i18n.Wrap(ErrSourceUnavailable, "source.empty_path", nil)
	}
	if !filepath.IsAbs(path) && baseDir != "" {
		path = filepath.Join(baseDir, path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", i18n.Wrap(ErrSourceUnavailable, "source.unreadable", map[string]any{"Path": path, "Error": err.Error()})
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", i18n.Wrap(ErrSourceUnavailable, "source.unreadable", map[string]any{"Path": abs, "Error": err.Error()})
	}
	if !info.Mode().IsRegular() {
		return "", i18n.Wrap(ErrSourceUnavailable, "source.not_regular", map[string]any{"Path": abs})
	}
	return Source(filePrefix + abs), nil
}

// Read returns the value: the file's contents for a file reference, and the
// string itself for an inline value. The result is held in memory only.
func (s Source) Read() ([]byte, error) {
	if !s.IsFile() {
		return []byte(s), nil
	}
	path := s.Path()
	if path == "" {
		return nil, i18n.Wrap(ErrSourceUnavailable, "source.empty_path", nil)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, i18n.Wrap(ErrSourceUnavailable, "source.unreadable", map[string]any{"Path": path, "Error": err.Error()})
	}
	return data, nil
}

// SecretSource is a Source for a secret. It behaves the same, except that an
// inline value is hidden when the type is printed or logged, so a secret does
// not leak through a stray format verb. Use Read for the value.
//
// An inline secret is still saved as written. A file reference is saved as an
// absolute path.
type SecretSource string

// IsFile reports whether s names a file.
func (s SecretSource) IsFile() bool { return Source(s).IsFile() }

// Path returns the path of a file reference, and "" for an inline value.
func (s SecretSource) Path() string { return Source(s).Path() }

// Read returns the value. See Source.Read.
func (s SecretSource) Read() ([]byte, error) { return Source(s).Read() }

// Resolve is Source.Resolve, keeping the SecretSource type.
func (s SecretSource) Resolve(baseDir string) (SecretSource, error) {
	r, err := Source(s).Resolve(baseDir)
	return SecretSource(r), err
}

// String returns a file reference as written and hides an inline value.
func (s SecretSource) String() string {
	switch {
	case s == "":
		return ""
	case s.IsFile():
		return string(s)
	default:
		return "(inline)"
	}
}
