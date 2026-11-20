// Package logging configures the CLI's diagnostic logging on top of log/slog.
//
// Logging is on by default at info level, to a rotated file in the CLI state
// directory. Credentials are redacted centrally and never reach a log. See
// spec.md for the full design.
package logging

import (
	"log/slog"
	"strconv"
	"strings"
)

// Environment variables that configure logging. Each one is the fallback for
// the matching Options field.
const (
	EnvLevel    = "IBM_AGENTID_LOG_LEVEL"
	EnvOutput   = "IBM_AGENTID_LOG_OUTPUT"
	EnvFile     = "IBM_AGENTID_LOG_FILE"
	EnvFormat   = "IBM_AGENTID_LOG_FORMAT"
	EnvMaxSize  = "IBM_AGENTID_LOG_MAX_SIZE"
	EnvMaxFiles = "IBM_AGENTID_LOG_MAX_FILES"
)

const (
	outputFile   = "file"
	outputStderr = "stderr"

	formatText = "text"
	formatJSON = "json"

	defaultMaxSizeMiB = 5
	defaultMaxFiles   = 5

	logDirName  = "logs"
	logFileName = "ibm-agentid.log"
)

// settings is the fully resolved logging configuration.
type settings struct {
	off      bool
	level    slog.Level
	output   string // outputFile or outputStderr
	file     string // explicit path; "" means the default location
	format   string // formatText or formatJSON
	maxSize  int64  // bytes
	maxFiles int

	// explicit reports whether the user set any logging option, either through
	// Options or the environment. Only explicit configuration produces warnings.
	explicit bool

	// invalid lists the values that were rejected and replaced by a default.
	invalid []invalidValue
}

type invalidValue struct {
	setting string
	value   string
	def     string
}

// resolve combines opts, the environment and the defaults. For every setting
// an explicit option wins over the environment, which wins over the default.
func resolve(opts Options, getenv func(string) string) settings {
	s := settings{
		level:    slog.LevelInfo,
		output:   outputFile,
		format:   formatText,
		maxSize:  defaultMaxSizeMiB << 20,
		maxFiles: defaultMaxFiles,
	}

	trim := strings.TrimSpace
	get := func(opt, env string) string {
		if v := trim(opt); v != "" {
			return v
		}
		return trim(getenv(env))
	}

	levelRaw := get(opts.Level, EnvLevel)
	formatRaw := get(opts.Format, EnvFormat)
	sizeRaw := get(opts.MaxSize, EnvMaxSize)
	filesRaw := get(opts.MaxFiles, EnvMaxFiles)

	// A file path implies file output unless an output was named at the same
	// or a higher precedence level.
	optOut, envOut := trim(opts.Output), trim(getenv(EnvOutput))
	optFile, envFile := trim(opts.File), trim(getenv(EnvFile))
	var outRaw string
	switch {
	case optOut != "":
		outRaw = optOut
	case optFile != "":
		outRaw = outputFile
	case envOut != "":
		outRaw = envOut
	case envFile != "":
		outRaw = outputFile
	}
	s.file = optFile
	if s.file == "" {
		s.file = envFile
	}

	s.explicit = levelRaw != "" || formatRaw != "" || sizeRaw != "" ||
		filesRaw != "" || outRaw != "" || s.file != ""

	reject := func(setting, value, def string) {
		s.invalid = append(s.invalid, invalidValue{setting: setting, value: value, def: def})
	}

	if levelRaw != "" {
		switch strings.ToLower(levelRaw) {
		case "off":
			s.off = true
		case "error":
			s.level = slog.LevelError
		case "warn", "warning":
			s.level = slog.LevelWarn
		case "info":
			s.level = slog.LevelInfo
		case "debug":
			s.level = slog.LevelDebug
		default:
			reject(EnvLevel, levelRaw, "info")
		}
	}

	if outRaw != "" {
		switch strings.ToLower(outRaw) {
		case outputFile:
			s.output = outputFile
		case outputStderr:
			s.output = outputStderr
		default:
			reject(EnvOutput, outRaw, outputFile)
		}
	}

	if formatRaw != "" {
		switch strings.ToLower(formatRaw) {
		case formatText:
			s.format = formatText
		case formatJSON:
			s.format = formatJSON
		default:
			reject(EnvFormat, formatRaw, formatText)
		}
	}

	if sizeRaw != "" {
		if n, err := strconv.Atoi(sizeRaw); err == nil && n >= 1 {
			s.maxSize = int64(n) << 20
		} else {
			reject(EnvMaxSize, sizeRaw, strconv.Itoa(defaultMaxSizeMiB))
		}
	}

	if filesRaw != "" {
		if n, err := strconv.Atoi(filesRaw); err == nil && n >= 0 {
			s.maxFiles = n
		} else {
			reject(EnvMaxFiles, filesRaw, strconv.Itoa(defaultMaxFiles))
		}
	}

	return s
}
