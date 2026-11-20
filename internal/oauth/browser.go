package oauth

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

// openBrowser opens rawURL in the default browser. The URL is a single
// argument to the platform command, never passed through a shell, and only
// http and https URLs are accepted.
func openBrowser(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("refusing to open a non-http URL")
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the child
	return nil
}
