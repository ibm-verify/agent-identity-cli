package logging

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// openLogFile opens path for appending, creating its directory (0700) and the
// file (0600) as needed. A file that is larger than maxSize is rotated first,
// keeping at most maxFiles older files. A file with broader permissions is
// tightened to 0600.
func openLogFile(path string, maxSize int64, maxFiles int) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return nil, err
	}

	if fi, err := os.Stat(path); err == nil && fi.Size() > maxSize {
		rotate(path, maxFiles)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, filePerm)
	if err != nil {
		return nil, err
	}

	if fi, err := f.Stat(); err == nil && fi.Mode().Perm()&^filePerm != 0 {
		_ = f.Chmod(filePerm)
	}
	return f, nil
}

// rotate shifts path to path.1, path.1 to path.2, and so on, and deletes
// anything beyond maxFiles. Failures are ignored: rotation is best effort and
// must never stop the command. Two processes rotating at once can lose a
// rotated file but cannot corrupt the live log.
func rotate(path string, maxFiles int) {
	removeNumberedBeyond(path, maxFiles)

	if maxFiles <= 0 {
		_ = os.Remove(path)
		return
	}

	_ = os.Remove(path + "." + strconv.Itoa(maxFiles))
	for i := maxFiles - 1; i >= 1; i-- {
		_ = os.Rename(path+"."+strconv.Itoa(i), path+"."+strconv.Itoa(i+1))
	}
	_ = os.Rename(path, path+".1")
}

// removeNumberedBeyond deletes path.N for every N greater than keep, which
// cleans up after the user lowers the number of files kept.
func removeNumberedBeyond(path string, keep int) {
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return
	}

	prefix := filepath.Base(path) + "."
	for _, e := range entries {
		suffix, ok := strings.CutPrefix(e.Name(), prefix)
		if !ok {
			continue
		}
		if n, err := strconv.Atoi(suffix); err == nil && n > keep {
			_ = os.Remove(filepath.Join(filepath.Dir(path), e.Name()))
		}
	}
}
