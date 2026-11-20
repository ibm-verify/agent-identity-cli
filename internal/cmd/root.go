// Package cmd defines the ibm-agentid commands. Each command is a function
// that returns a cobra command; flags are read into plain structs and the work
// is done by ordinary functions.
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

// options holds what every command shares.
type options struct {
	// context is the --context flag.
	context string
}

// NewRoot returns the root command with every subcommand attached. Cobra's own
// usage and error printing are off, so the caller prints the returned error
// once. Help text is rendered here, so call i18n.SetLanguage first.
func NewRoot(version string) *cobra.Command {
	opts := &options{}

	root := &cobra.Command{
		Use:           "ibm-agentid",
		Short:         i18n.T("root.short", nil),
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&opts.context, "context", "", i18n.T("root.context_flag", nil))

	root.AddCommand(newLogin(opts), newContext(opts))
	root.AddCommand(newGet(opts), newCreate(opts), newReplace(opts), newPatch(opts), newDelete(opts))
	return root
}
