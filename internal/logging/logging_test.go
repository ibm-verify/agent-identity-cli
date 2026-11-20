package logging

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// isolate points the state directory at a temporary home, clears every
// logging variable, and restores the default logger afterwards.
func isolate(t *testing.T) (stateDir string) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	for _, k := range []string{EnvLevel, EnvOutput, EnvFile, EnvFormat, EnvMaxSize, EnvMaxFiles,
		"IBM_AGENTID_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}

	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	return filepath.Join(home, ".ibm-agentid")
}

func TestInitDefaultsToInfoFile(t *testing.T) {
	state := isolate(t)

	var warn bytes.Buffer
	cleanup := Init(Options{Version: "1.2.3", Args: []string{"/usr/bin/ibm-agentid", "get", "agents"}, Stderr: &warn})
	slog.Debug("not recorded at info")
	slog.Info("something happened", "context", "prod")
	cleanup()

	path := filepath.Join(state, "logs", "ibm-agentid.log")
	log := readFile(t, path)

	for _, want := range []string{
		"cli started", "version=1.2.3", "command=\"ibm-agentid get agents\"",
		"something happened", "context=prod", "pid=",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log should contain %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "not recorded at info") {
		t.Errorf("debug record must not appear at info:\n%s", log)
	}
	if warn.Len() != 0 {
		t.Errorf("a default run must not warn, got %q", warn.String())
	}

	if runtime.GOOS != "windows" {
		di, _ := os.Stat(filepath.Dir(path))
		fi, _ := os.Stat(path)
		if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
			t.Errorf("perms dir=%o file=%o, want 700/600", di.Mode().Perm(), fi.Mode().Perm())
		}
	}
}

func TestInitLevelOffCreatesNothing(t *testing.T) {
	state := isolate(t)
	t.Setenv(EnvLevel, "off")

	cleanup := Init(Options{})
	slog.Error("nobody will see this")
	cleanup()

	if exists(state) {
		t.Error("level off must not create the state directory")
	}
	if slog.Default().Enabled(context.Background(), slog.LevelError) {
		t.Error("the discard handler must report every level as disabled")
	}
}

func TestInitDebugAndLevels(t *testing.T) {
	state := isolate(t)

	cleanup := Init(Options{Level: "debug"})
	slog.Debug("decision made")
	cleanup()
	if log := readFile(t, filepath.Join(state, "logs", "ibm-agentid.log")); !strings.Contains(log, "decision made") {
		t.Errorf("debug record missing:\n%s", log)
	}

	path := filepath.Join(t.TempDir(), "err.log")
	cleanup = Init(Options{Level: "error", File: path})
	slog.Warn("a warning")
	slog.Error("an error")
	cleanup()
	log := readFile(t, path)
	if strings.Contains(log, "a warning") || !strings.Contains(log, "an error") {
		t.Errorf("error level should keep only errors:\n%s", log)
	}
}

func TestInitStderrOutput(t *testing.T) {
	state := isolate(t)

	var buf bytes.Buffer
	cleanup := Init(Options{Output: "stderr", Stderr: &buf})
	slog.Info("to stderr")
	cleanup()

	if !strings.Contains(buf.String(), "to stderr") {
		t.Errorf("stderr output missing: %q", buf.String())
	}
	if exists(state) {
		t.Error("stderr output must not create a log file")
	}
}

func TestInitJSONFormat(t *testing.T) {
	isolate(t)
	t.Setenv(EnvFormat, "json")

	var buf bytes.Buffer
	cleanup := Init(Options{Output: "stderr", Stderr: &buf})
	slog.Info("structured", "k", "v")
	cleanup()

	if !strings.Contains(buf.String(), `"msg":"structured"`) || !strings.Contains(buf.String(), `"k":"v"`) {
		t.Errorf("not JSON: %q", buf.String())
	}
}

func TestInitRotatesOnStart(t *testing.T) {
	state := isolate(t)
	path := filepath.Join(state, "logs", "ibm-agentid.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, strings.Repeat("x", 2<<20)) // 2 MiB

	t.Setenv(EnvMaxSize, "1")
	Init(Options{})()

	if !exists(path + ".1") {
		t.Error("an over-size log should have been rotated at start-up")
	}
}

// unwritableState makes the state directory impossible to create by putting a
// regular file where the directory should be.
func unwritableState(t *testing.T, state string) {
	t.Helper()
	if err := os.WriteFile(state, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInitFailureIsSilentUnlessExplicit(t *testing.T) {
	state := isolate(t)
	unwritableState(t, state)

	// Nothing configured: logging was not asked for, so stay quiet.
	var quiet bytes.Buffer
	cleanup := Init(Options{Stderr: &quiet})
	slog.Info("dropped")
	cleanup()
	if quiet.Len() != 0 {
		t.Errorf("default run must be silent, got %q", quiet.String())
	}

	// A level set explicitly: the user wants logs, so say they are unavailable.
	var warned bytes.Buffer
	cleanup = Init(Options{Level: "debug", Stderr: &warned})
	slog.Info("dropped")
	cleanup()
	if !strings.Contains(warned.String(), "could not open the log file") {
		t.Errorf("explicit run should warn, got %q", warned.String())
	}
	if strings.Count(warned.String(), "\n") != 1 {
		t.Errorf("exactly one warning expected, got %q", warned.String())
	}
}

func TestInitFailureDisablesLogging(t *testing.T) {
	state := isolate(t)
	unwritableState(t, state)

	Init(Options{})()
	if slog.Default().Enabled(context.Background(), slog.LevelError) {
		t.Error("logging should be disabled after a failure to open the log")
	}
}

func TestInitWarnsOnInvalidSettingsOnlyWhenExplicit(t *testing.T) {
	isolate(t)

	var buf bytes.Buffer
	Init(Options{Level: "loud", Output: "stderr", Stderr: &buf})()

	if !strings.Contains(buf.String(), `invalid value "loud" for IBM_AGENTID_LOG_LEVEL`) {
		t.Errorf("missing warning: %q", buf.String())
	}
}

func TestCommandLineScrubsFlagValues(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"/usr/local/bin/ibm-agentid", "get", "agents"}, "ibm-agentid get agents"},
		{[]string{"ibm-agentid", "login", "-f", "env.yaml", "--context", "prod"}, "ibm-agentid login -f env.yaml --context"},
		{[]string{"ibm-agentid", "login", "--file=/etc/env.yaml"}, "ibm-agentid login --file=/etc/env.yaml"},
		{[]string{"ibm-agentid", "login", "-"}, "ibm-agentid login -"},
		{[]string{"ibm-agentid", "login", "--client-secret=hunter2"}, "ibm-agentid login --client-secret"},
		{[]string{"ibm-agentid", "login", "--client-secret", "hunter2", "--no-save"}, "ibm-agentid login --client-secret --no-save"},
		{[]string{"ibm-agentid", "context", "use", "prod"}, "ibm-agentid context use prod"},
		{[]string{"ibm-agentid"}, "ibm-agentid"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := commandLine(tt.args); got != tt.want {
			t.Errorf("commandLine(%v) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestInitStartupRecordHasNoFlagValues(t *testing.T) {
	isolate(t)

	var buf bytes.Buffer
	Init(Options{
		Output: "stderr", Stderr: &buf,
		Args: []string{"ibm-agentid", "login", "--client-secret", "TOPSECRETVALUE", "--token=ALSOSECRET"},
	})()

	for _, leaked := range []string{"TOPSECRETVALUE", "ALSOSECRET"} {
		if strings.Contains(buf.String(), leaked) {
			t.Errorf("%q leaked into the start-up record: %s", leaked, buf.String())
		}
	}
}

func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	state := isolate(t)

	cleanup := Init(Options{})
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				slog.Info("concurrent", "goroutine", g, "payload", strings.Repeat("y", 200))
			}
		}()
	}
	wg.Wait()
	cleanup()

	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(readFile(t, filepath.Join(state, "logs", "ibm-agentid.log"))), "\n") {
		if !strings.Contains(line, "concurrent") {
			continue
		}
		lines++
		if got := strings.Count(line, "time="); got != 1 {
			t.Fatalf("interleaved line (%d time= markers): %q", got, line)
		}
		if !strings.HasSuffix(line, strings.Repeat("y", 200)) {
			t.Fatalf("truncated or mixed line: %q", line)
		}
	}
	if lines != 8*50 {
		t.Errorf("got %d concurrent lines, want %d", lines, 8*50)
	}
}
