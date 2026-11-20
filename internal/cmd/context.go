package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// newContext returns the context command. Like login, it writes only data to
// stdout (names, the table, the settings). Confirmations go to stderr.
func newContext(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "context",
		Short: i18n.T("context.short", nil),
		Long:  i18n.T("context.long", nil),
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "list",
			Short: i18n.T("context.list_short", nil),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runContextList(cmd)
			},
		},
		&cobra.Command{
			Use:   "use <name>",
			Short: i18n.T("context.use_short", nil),
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runContextUse(cmd, args[0])
			},
		},
		&cobra.Command{
			Use:   "current",
			Short: i18n.T("context.current_short", nil),
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return runContextCurrent(cmd, opts)
			},
		},
		&cobra.Command{
			Use:   "view [name]",
			Short: i18n.T("context.view_short", nil),
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				name := opts.context
				if len(args) == 1 {
					name = args[0]
				}
				return runContextView(cmd, name)
			},
		},
		&cobra.Command{
			Use:   "rename <old> <new>",
			Short: i18n.T("context.rename_short", nil),
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				return runContextRename(cmd, args[0], args[1])
			},
		},
		newContextDelete(),
	)
	return cmd
}

func newContextDelete() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: i18n.T("context.delete_short", nil),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContextDelete(cmd, args[0], yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, i18n.T("context.yes_flag", nil))
	return cmd
}

func runContextList(cmd *cobra.Command) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	if len(cfg.Contexts) == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("context.list_empty", nil))
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, " \t%s\t%s\t%s\n", i18n.T("context.col_name", nil), i18n.T("context.col_server", nil), i18n.T("context.col_token", nil))
	for _, name := range cfg.Names() {
		env := cfg.Contexts[name]
		mark := " "
		if name == cfg.CurrentContext {
			mark = "*"
		}
		_, status, err := profile.LookupToken(name, env, time.Now())
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", mark, name, env.Server, tokenState(status))
	}
	return w.Flush()
}

func tokenState(s profile.Status) string {
	switch s {
	case profile.TokenValid:
		return i18n.T("context.token_valid", nil)
	case profile.TokenExpired:
		return i18n.T("context.token_expired", nil)
	case profile.TokenStale:
		return i18n.T("context.token_stale", nil)
	}
	return i18n.T("context.token_none", nil)
}

func runContextUse(cmd *cobra.Command, name string) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	if err := cfg.Use(name); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("context.used", map[string]any{"Name": name}))
	return nil
}

// runContextCurrent prints the context that commands would use, which honors
// --context and IBM_AGENTID_CONTEXT as well as the saved current context.
func runContextCurrent(cmd *cobra.Command, opts *options) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), cfg.ResolveName(opts.context))
	return err
}

func runContextView(cmd *cobra.Command, name string) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	_, env, err := cfg.Resolve(name)
	if err != nil {
		return err
	}
	return writeYAML(cmd.OutOrStdout(), maskSecrets(env))
}

// maskSecrets returns a copy of env with inline secrets hidden. SecretSource
// hides an inline value when printed, but the YAML encoder writes the raw
// string, so it is replaced here. A file reference is kept as written.
func maskSecrets(env *profile.Environment) *profile.Environment {
	c := *env
	c.OAuth.PrivateJWK = profile.SecretSource(env.OAuth.PrivateJWK.String())
	c.OAuth.ClientSecret = profile.SecretSource(env.OAuth.ClientSecret.String())
	return &c
}

func writeYAML(w io.Writer, v any) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return enc.Close()
}

func runContextRename(cmd *cobra.Command, oldName, newName string) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	if err := cfg.Rename(oldName, newName); err != nil {
		return err
	}
	if err := profile.RenameToken(oldName, newName); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("context.renamed", map[string]any{"Old": oldName, "New": newName}))
	return nil
}

func runContextDelete(cmd *cobra.Command, name string, yes bool) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	if _, err := cfg.Get(name); err != nil {
		return err
	}
	if !yes && !confirm(cmd, i18n.T("context.delete_confirm", map[string]any{"Name": name})) {
		return i18n.Errorf("context.delete_aborted", nil)
	}
	if err := cfg.Delete(name); err != nil {
		return err
	}
	if err := profile.DeleteToken(name); err != nil {
		return err
	}
	fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("context.deleted", map[string]any{"Name": name}))
	return nil
}

// confirm asks on stderr and reads one line from stdin. Only "y" or "yes"
// confirms, so a script with no input never deletes anything.
func confirm(cmd *cobra.Command, prompt string) bool {
	fmt.Fprint(cmd.ErrOrStderr(), prompt)
	line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
