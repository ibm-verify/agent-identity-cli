package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// Options carries the same settings as the command-line flags. Every field is
// a string so that "unset" can be told apart from a real value; the
// environment fills in whatever is empty.
type Options struct {
	Level    string // "", "off", "error", "warn", "info", "debug"
	Output   string // "", "file", "stderr"
	File     string // explicit file path; implies Output "file"
	Format   string // "", "text", "json"
	MaxSize  string // MiB; "" uses the environment or the default
	MaxFiles string // rotated files kept; "" uses the environment or the default

	// Version is the CLI version recorded in the start-up record.
	Version string
	// Args is the command line recorded in the start-up record, flag values
	// removed. It defaults to os.Args.
	Args []string
	// Stderr receives stderr output and warnings. It defaults to os.Stderr.
	Stderr io.Writer
}

// Init configures the default slog logger from opts and the environment and
// returns a function that closes the log file. It never fails the command: if
// the destination cannot be opened, logging is disabled, with a warning only
// when the user explicitly configured logging. Call it once, early in start-up,
// and defer the returned function.
func Init(opts Options) (cleanup func()) {
	s := resolve(opts, os.Getenv)

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	if s.explicit {
		for _, iv := range s.invalid {
			fmt.Fprintln(stderr, i18n.T("logging.invalid_setting", map[string]any{
				"Setting": iv.setting,
				"Value":   iv.value,
				"Default": iv.def,
			}))
		}
	}

	disable := func() func() {
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return func() {}
	}

	if s.off {
		return disable()
	}

	var (
		w       io.Writer
		closeFn = func() {}
	)
	switch s.output {
	case outputStderr:
		w = stderr
	default:
		path, err := logPath(s.file)
		if err == nil {
			var f *os.File
			if f, err = openLogFile(path, s.maxSize, s.maxFiles); err == nil {
				w, closeFn = f, func() { _ = f.Close() }
			}
		}
		if err != nil {
			// Logging is on by default, so a log that cannot be opened (a
			// read-only home directory, for example) is only worth mentioning
			// when the user asked for it.
			if s.explicit {
				fmt.Fprintln(stderr, i18n.T("logging.open_failed", map[string]any{
					"Path":  path,
					"Error": err.Error(),
				}))
			}
			return disable()
		}
	}

	hopts := &slog.HandlerOptions{Level: s.level, ReplaceAttr: replaceAttr}
	var h slog.Handler
	if s.format == formatJSON {
		h = slog.NewJSONHandler(w, hopts)
	} else {
		h = slog.NewTextHandler(w, hopts)
	}
	slog.SetDefault(slog.New(h).With("pid", os.Getpid()))

	args := opts.Args
	if args == nil {
		args = os.Args
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	slog.Info("cli started",
		"version", version,
		"os", runtime.GOOS,
		"arch", runtime.GOARCH,
		"command", commandLine(args),
	)

	return closeFn
}

// logPath returns the log file to use: the explicit path, or the default file
// in the CLI state directory.
func logPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	dir, err := profile.Dir()
	if err != nil {
		return filepath.Join(logDirName, logFileName), err
	}
	return filepath.Join(dir, logDirName, logFileName), nil
}

// commandLine renders args without any flag values: the executable name, the
// subcommand words that precede the first flag, and flag names. The one
// exception is the value of -f/--file, a path that helps troubleshooting and
// is not secret; "-" (stdin) is kept as is.
func commandLine(args []string) string {
	if len(args) == 0 {
		return ""
	}

	out := []string{filepath.Base(args[0])}
	seenFlag := false
	for i := 1; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-":
			out = append(out, "-")
		case strings.HasPrefix(a, "-"):
			seenFlag = true
			name, value, hasValue := strings.Cut(a, "=")
			if name == "-f" || name == "--file" {
				if hasValue {
					out = append(out, name+"="+value)
				} else {
					out = append(out, name)
					if i+1 < len(args) {
						i++
						out = append(out, args[i])
					}
				}
				continue
			}
			out = append(out, name)
		case !seenFlag:
			out = append(out, a) // subcommand path
		}
		// A word after a flag is that flag's value (or an argument) and is dropped.
	}
	return strings.Join(out, " ")
}
