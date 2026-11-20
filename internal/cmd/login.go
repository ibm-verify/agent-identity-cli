package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/oauth"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

type loginFlags struct {
	dataFlags
	noBrowser  bool
	noSave     bool
	printToken bool
	timeout    time.Duration
}

func newLogin(opts *options) *cobra.Command {
	var f loginFlags

	// Use and Example are not translated: the first word of Use is the command
	// name, and Example holds only commands.
	cmd := &cobra.Command{
		Use:   "login (-d <file|-> | --template)",
		Short: i18n.T("login.short", nil),
		Long:  i18n.T("login.long", nil),
		Example: `  ibm-agentid login --template > prod.yaml
  ibm-agentid login -d prod.yaml --context prod
  ibm-agentid login -d - --no-save --print-token < ci.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd, opts, &f)
		},
	}

	f.add(cmd)
	fl := cmd.Flags()
	fl.BoolVar(&f.noBrowser, "no-browser", false, i18n.T("login.no_browser_flag", nil))
	fl.BoolVar(&f.noSave, "no-save", false, i18n.T("login.no_save_flag", nil))
	fl.BoolVar(&f.printToken, "print-token", false, i18n.T("login.print_token_flag", nil))
	fl.DurationVar(&f.timeout, "timeout", 0, i18n.T("login.timeout_flag", nil))
	return cmd
}

// runLogin writes only the template or the token to stdout. Everything else
// goes to stderr.
func runLogin(cmd *cobra.Command, opts *options, f *loginFlags) error {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	if f.template {
		b, err := input.LoginTemplate(f.output)
		if err != nil {
			return err
		}
		_, err = out.Write(b)
		return err
	}
	if f.file == "" {
		return i18n.Wrap(input.ErrInvalidInput, "login.data_required", nil)
	}

	data, err := input.Read(f.file, os.Stdin)
	if err != nil {
		return err
	}
	login, err := input.ParseLogin(data, opts.context)
	if err != nil {
		return err
	}
	if !f.noSave {
		for _, prop := range login.InlineSecrets() {
			fmt.Fprintln(errOut, i18n.T("login.inline_secret", map[string]any{"Property": prop}))
		}
	}

	p := &oauth.Provider{Context: login.Context, Env: &login.Environment, Out: errOut}
	tok, err := p.Login(cmd.Context(), oauth.LoginOptions{NoBrowser: f.noBrowser, NoSave: f.noSave, Timeout: f.timeout})
	if err != nil {
		// A token that could not be cached is still worth printing.
		if tok != nil && f.printToken {
			fmt.Fprintln(out, tok.AccessToken)
		}
		return err
	}

	if !f.noSave {
		if err := saveContext(login); err != nil {
			// The login cached a token for a context that does not exist.
			_ = profile.DeleteToken(login.Context)
			return err
		}
	}

	if f.printToken {
		fmt.Fprintln(out, tok.AccessToken)
	}
	if f.noSave {
		fmt.Fprintln(errOut, i18n.T("login.success_no_save", nil))
	} else {
		fmt.Fprintln(errOut, i18n.T("login.success", map[string]any{"Name": login.Context}))
	}
	return nil
}

// saveContext saves the environment under its name and makes it current.
func saveContext(l *input.Login) error {
	cfg, err := profile.Load()
	if err != nil {
		return err
	}
	if err := cfg.Set(l.Context, &l.Environment); err != nil {
		return err
	}
	return cfg.Use(l.Context)
}
