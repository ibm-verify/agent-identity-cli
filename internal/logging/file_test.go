package logging

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestOpenLogFileCreatesWithStrictPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "state", "logs", "ibm-agentid.log")

	f, err := openLogFile(path, 1<<20, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perm = %o, want 700", di.Mode().Perm())
	}
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file perm = %o, want 600", fi.Mode().Perm())
	}
}

func TestOpenLogFileTightensBroadPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "old\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	f, err := openLogFile(path, 1<<20, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("perm = %o, want 600", fi.Mode().Perm())
	}
}

func TestOpenLogFileAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "first\n")

	f, err := openLogFile(path, 1<<20, 5)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("second\n")
	f.Close()

	if got := readFile(t, path); got != "first\nsecond\n" {
		t.Errorf("got %q", got)
	}
}

func TestRotationShiftsFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "current-oversized")
	for i, name := range []string{".1", ".2", ".3", ".4", ".5"} {
		writeFile(t, path+name, "old"+string(rune('1'+i)))
	}

	f, err := openLogFile(path, 5, 5) // current file is larger than 5 bytes
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	want := map[string]string{
		path:        "",
		path + ".1": "current-oversized",
		path + ".2": "old1",
		path + ".3": "old2",
		path + ".4": "old3",
		path + ".5": "old4", // the previous .5 ("old5") is gone
	}
	for p, content := range want {
		if got := readFile(t, p); got != content {
			t.Errorf("%s = %q, want %q", filepath.Base(p), got, content)
		}
	}
	if exists(path + ".6") {
		t.Error("no file beyond .5 may exist")
	}
}

func TestRotationNotTriggeredBelowMaxSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "small")

	f, err := openLogFile(path, 100, 5)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if exists(path + ".1") {
		t.Error("a file under the limit must not rotate")
	}
	if got := readFile(t, path); got != "small" {
		t.Errorf("content changed: %q", got)
	}
}

func TestRotationWithNoHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "oversized-content")
	writeFile(t, path+".1", "stale")

	f, err := openLogFile(path, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if exists(path + ".1") {
		t.Error("MaxFiles=0 keeps no rotated files")
	}
	if got := readFile(t, path); got != "" {
		t.Errorf("the full file should be discarded, got %q", got)
	}
}

func TestRotationPrunesWhenLimitIsLowered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.log")
	writeFile(t, path, "current-oversized")
	for _, name := range []string{".1", ".2", ".3", ".4", ".5"} {
		writeFile(t, path+name, "old"+name)
	}

	f, err := openLogFile(path, 5, 2)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if !exists(path+".1") || !exists(path+".2") {
		t.Error(".1 and .2 should be kept")
	}
	for _, name := range []string{".3", ".4", ".5"} {
		if exists(path + name) {
			t.Errorf("%s should have been pruned", name)
		}
	}
	if got := readFile(t, path+".1"); got != "current-oversized" {
		t.Errorf(".1 = %q", got)
	}
	if got := readFile(t, path+".2"); got != "old.1" {
		t.Errorf(".2 = %q", got)
	}
}

func TestRotationIgnoresUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.log")
	writeFile(t, path, "current-oversized")
	writeFile(t, filepath.Join(dir, "x.log.bak"), "keep")
	writeFile(t, filepath.Join(dir, "other.log.9"), "keep")

	f, err := openLogFile(path, 5, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	if !exists(filepath.Join(dir, "x.log.bak")) || !exists(filepath.Join(dir, "other.log.9")) {
		t.Error("rotation must only touch numbered siblings of the log file")
	}
}
