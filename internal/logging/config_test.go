package logging

import (
	"log/slog"
	"testing"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveDefaults(t *testing.T) {
	s := resolve(Options{}, envMap(nil))

	if s.off || s.level != slog.LevelInfo || s.output != outputFile || s.format != formatText {
		t.Errorf("unexpected defaults: %+v", s)
	}
	if s.maxSize != 5<<20 || s.maxFiles != 5 {
		t.Errorf("unexpected rotation defaults: size=%d files=%d", s.maxSize, s.maxFiles)
	}
	if s.explicit || len(s.invalid) != 0 {
		t.Errorf("defaults must not be explicit or invalid: %+v", s)
	}
}

func TestResolvePrecedence(t *testing.T) {
	env := envMap(map[string]string{
		EnvLevel:    "warn",
		EnvOutput:   "stderr",
		EnvFormat:   "json",
		EnvMaxSize:  "2",
		EnvMaxFiles: "7",
	})

	// Environment beats default.
	s := resolve(Options{}, env)
	if s.level != slog.LevelWarn || s.output != outputStderr || s.format != formatJSON ||
		s.maxSize != 2<<20 || s.maxFiles != 7 || !s.explicit {
		t.Errorf("environment not applied: %+v", s)
	}

	// Options beat the environment.
	s = resolve(Options{Level: "debug", Output: "file", Format: "text", MaxSize: "9", MaxFiles: "1"}, env)
	if s.level != slog.LevelDebug || s.output != outputFile || s.format != formatText ||
		s.maxSize != 9<<20 || s.maxFiles != 1 {
		t.Errorf("options not applied over the environment: %+v", s)
	}
}

func TestResolveLevels(t *testing.T) {
	tests := map[string]slog.Level{
		"error": slog.LevelError, "warn": slog.LevelWarn, "WARNING": slog.LevelWarn,
		"info": slog.LevelInfo, "Debug": slog.LevelDebug,
	}
	for in, want := range tests {
		if s := resolve(Options{Level: in}, envMap(nil)); s.level != want || s.off {
			t.Errorf("level %q: got %v off=%v, want %v", in, s.level, s.off, want)
		}
	}
	if s := resolve(Options{Level: "off"}, envMap(nil)); !s.off {
		t.Error("level off should disable logging")
	}
}

func TestResolveFileImpliesFileOutput(t *testing.T) {
	// A file path alone selects file output even when the environment asks for stderr...
	s := resolve(Options{File: "/tmp/x.log"}, envMap(map[string]string{EnvOutput: "stderr"}))
	if s.output != outputFile || s.file != "/tmp/x.log" {
		t.Errorf("option file should beat environment output: %+v", s)
	}

	// ...but an explicit output at the same level wins.
	s = resolve(Options{File: "/tmp/x.log", Output: "stderr"}, envMap(nil))
	if s.output != outputStderr {
		t.Errorf("explicit output should win: %+v", s)
	}

	// An environment file path implies file output.
	s = resolve(Options{}, envMap(map[string]string{EnvFile: "/tmp/y.log"}))
	if s.output != outputFile || s.file != "/tmp/y.log" || !s.explicit {
		t.Errorf("environment file: %+v", s)
	}
}

func TestResolveInvalidValuesFallBack(t *testing.T) {
	s := resolve(Options{
		Level: "loud", Output: "pigeon", Format: "xml", MaxSize: "0", MaxFiles: "-1",
	}, envMap(nil))

	if s.level != slog.LevelInfo || s.off || s.output != outputFile || s.format != formatText ||
		s.maxSize != 5<<20 || s.maxFiles != 5 {
		t.Errorf("invalid values must fall back to defaults: %+v", s)
	}
	if len(s.invalid) != 5 || !s.explicit {
		t.Errorf("want 5 rejected values and explicit=true, got %d / %v", len(s.invalid), s.explicit)
	}

	for _, bad := range []string{"abc", "1.5", ""} {
		got := resolve(Options{MaxFiles: bad}, envMap(nil))
		if bad != "" && got.maxFiles != 5 {
			t.Errorf("MaxFiles %q: got %d, want default 5", bad, got.maxFiles)
		}
	}
	if s := resolve(Options{MaxFiles: "0"}, envMap(nil)); s.maxFiles != 0 || len(s.invalid) != 0 {
		t.Errorf("MaxFiles 0 is valid: %+v", s)
	}
}
