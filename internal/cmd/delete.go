package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

type deleteFlags struct {
	target
	yes bool
}

// newDelete returns delete, with a subcommand for each resource that can be
// deleted. It asks before deleting. Nothing is written to stdout.
func newDelete(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: i18n.T("delete.short", nil),
		Long:  i18n.T("delete.long", nil),
	}
	for _, r := range resources() {
		if r.Delete != nil {
			cmd.AddCommand(newDeleteResource(opts, r))
		}
	}
	return cmd
}

func newDeleteResource(opts *options, r *resource.Resource) *cobra.Command {
	var f deleteFlags
	cmd := &cobra.Command{
		Use:     r.Name + " (<id> | --name <name>)",
		Aliases: r.Aliases,
		Short:   i18n.T("delete.resource_short", map[string]any{"Resource": r.Name}),
		Example: fmt.Sprintf("  ibm-agentid delete %[1]s <id>\n  ibm-agentid delete %[1]s --name <name> --yes", r.Name),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd, opts, r, &f, args)
		},
	}
	f.addFlag(cmd)
	cmd.Flags().BoolVarP(&f.yes, "yes", "y", false, i18n.T("delete.yes_flag", nil))
	return cmd
}

func runDelete(cmd *cobra.Command, opts *options, r *resource.Resource, f *deleteFlags, args []string) error {
	if err := f.parse(r, args, true); err != nil {
		return err
	}
	c, err := connect(cmd, opts)
	if err != nil {
		return err
	}
	id, err := f.resolve(cmd, r, c)
	if err != nil {
		return err
	}

	shown := id
	if f.name != "" {
		shown = f.name
	}
	prompt := i18n.T("delete.confirm", map[string]any{"Singular": r.Singular, "Target": shown})
	if !f.yes && !confirm(cmd, prompt) {
		return i18n.Wrap(resource.ErrNotConfirmed, "delete.aborted", nil)
	}

	slog.Info("delete", "resource", r.Name, "id", id)
	if err := r.Delete(cmd.Context(), c.Conn, id); err != nil {
		return c.explain(cmd, err)
	}
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("delete.done", map[string]any{"Singular": r.Singular, "ID": id}))
	return nil
}
