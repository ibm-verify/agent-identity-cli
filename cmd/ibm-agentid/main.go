// Command ibm-agentid is the command-line client for IBM Agent Identity.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/ibm-verify/agent-identity-cli/internal/cmd"
	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/logging"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

// run is main without os.Exit, so deferred cleanup runs.
func run() int {
	i18n.SetLanguage("")
	defer logging.Init(logging.Options{Version: version})()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := cmd.NewRoot(version).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
