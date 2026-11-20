package profile

import (
	"os"
	"path/filepath"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

const (
	dirName        = ".ibm-agentid"
	configFileName = "config.yaml"
	cacheDirName   = "cache"

	dirPerm  os.FileMode = 0o700
	filePerm os.FileMode = 0o600
)

// Dir returns the directory that holds all persistent CLI state. It is
// $XDG_CONFIG_HOME/.ibm-agentid when XDG_CONFIG_HOME is set, otherwise
// ~/.ibm-agentid. The directory is not created.
func Dir() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, dirName), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", i18n.Wrap(err, "config.home_dir_unresolved", map[string]any{"Error": err.Error()})
	}
	return filepath.Join(home, dirName), nil
}

// ConfigPath returns the path of config.yaml.
func ConfigPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

// CacheDir returns the directory that holds the per-context token cache files.
func CacheDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheDirName), nil
}

// writeFileAtomic writes data to path by writing a temporary file in the same
// directory and renaming it over the destination. The parent directory is
// created with 0700 if it does not exist.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	// Best-effort cleanup; after a successful rename the temp file no longer exists.
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(filePerm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, path)
}
