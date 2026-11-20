package cmd

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// writeVerb is what differs between create, replace and patch: the verb name,
// whether the command names a target, and the call. Everything else is the
// same, so they share runWrite.
type writeVerb struct {
	name   string // also the i18n prefix and the log verb
	target bool
	// call returns nil when the resource has no such verb.
	call func(r *resource.Resource) func(*cobra.Command, *connection, string, *resource.Payload) (*resource.Object, error)
	// emptyOK is false when an empty payload is an error.
	emptyOK bool
}

var (
	createVerb = writeVerb{
		name: "create", emptyOK: true,
		call: func(r *resource.Resource) func(*cobra.Command, *connection, string, *resource.Payload) (*resource.Object, error) {
			if r.Create == nil {
				return nil
			}
			return func(cmd *cobra.Command, c *connection, _ string, p *resource.Payload) (*resource.Object, error) {
				return r.Create(cmd.Context(), c.Conn, p)
			}
		},
	}
	replaceVerb = writeVerb{
		name: "replace", target: true, emptyOK: true,
		call: func(r *resource.Resource) func(*cobra.Command, *connection, string, *resource.Payload) (*resource.Object, error) {
			if r.Replace == nil {
				return nil
			}
			return func(cmd *cobra.Command, c *connection, id string, p *resource.Payload) (*resource.Object, error) {
				return r.Replace(cmd.Context(), c.Conn, id, p)
			}
		},
	}
	patchVerb = writeVerb{
		name: "patch", target: true,
		call: func(r *resource.Resource) func(*cobra.Command, *connection, string, *resource.Payload) (*resource.Object, error) {
			if r.Patch == nil {
				return nil
			}
			return func(cmd *cobra.Command, c *connection, id string, p *resource.Payload) (*resource.Object, error) {
				return r.Patch(cmd.Context(), c.Conn, id, p)
			}
		},
	}
)

func newCreate(opts *options) *cobra.Command  { return newWrite(opts, createVerb) }
func newReplace(opts *options) *cobra.Command { return newWrite(opts, replaceVerb) }
func newPatch(opts *options) *cobra.Command   { return newWrite(opts, patchVerb) }

// newWrite returns the parent command of a verb, with a subcommand for each
// resource that has the verb. Help texts of the three verbs are separate
// messages, because what they promise differs.
func newWrite(opts *options, v writeVerb) *cobra.Command {
	cmd := &cobra.Command{Use: v.name}
	switch v.name {
	case "create":
		cmd.Short, cmd.Long = i18n.T("create.short", nil), i18n.T("create.long", nil)
	case "replace":
		cmd.Short, cmd.Long = i18n.T("replace.short", nil), i18n.T("replace.long", nil)
	case "patch":
		cmd.Short, cmd.Long = i18n.T("patch.short", nil), i18n.T("patch.long", nil)
	}
	for _, r := range resources() {
		if v.call(r) != nil {
			cmd.AddCommand(newWriteResource(opts, v, r))
		}
	}
	return cmd
}

func newWriteResource(opts *options, v writeVerb, r *resource.Resource) *cobra.Command {
	var (
		f dataFlags
		t target
	)
	use := r.Name + " (-d <file|-> | --template)"
	args := cobra.NoArgs
	example := fmt.Sprintf("  ibm-agentid %[1]s %[2]s --template > %[2]s.yaml\n  ibm-agentid %[1]s %[2]s -d %[2]s.yaml", v.name, r.Name)
	if v.target {
		use = r.Name + " (<id> | --name <name>) (-d <file|-> | --template)"
		args = cobra.MaximumNArgs(1)
		example = fmt.Sprintf("  ibm-agentid %[1]s %[2]s <id> -d %[2]s.yaml\n  ibm-agentid %[1]s %[2]s --name <name> -d -", v.name, r.Name)
	}

	cmd := &cobra.Command{
		Use:     use,
		Aliases: r.Aliases,
		Short:   i18n.T("resource.write_short", map[string]any{"Resource": r.Name, "Verb": v.name}),
		Example: example,
		Args:    args,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWrite(cmd, opts, v, r, &f, &t, args)
		},
	}
	f.add(cmd)
	if v.target {
		t.addFlag(cmd)
	}
	return cmd
}

// runWrite prints the resulting object to stdout and the confirmation to
// stderr. The data is parsed, and an empty patch refused, before any token is
// fetched, so bad input fails without a network call.
func runWrite(cmd *cobra.Command, opts *options, v writeVerb, r *resource.Resource, f *dataFlags, t *target, args []string) error {
	if f.template {
		b, err := r.Template(f.output)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(b)
		return err
	}
	if err := checkFormat(f.output, "yaml", "json"); err != nil {
		return err
	}
	if f.file == "" {
		return i18n.Wrap(input.ErrInvalidInput, "resource.data_required", map[string]any{"Verb": v.name})
	}
	if v.target {
		if err := t.parse(r, args, true); err != nil {
			return err
		}
	}

	data, err := input.Read(f.file, os.Stdin)
	if err != nil {
		return err
	}
	p, err := r.Parse(data)
	if err != nil {
		return err
	}
	if !v.emptyOK && p.Empty() {
		return i18n.Wrap(input.ErrInvalidInput, "resource.patch_empty", nil)
	}

	c, err := connect(cmd, opts)
	if err != nil {
		return err
	}
	var id string
	if v.target {
		if id, err = t.resolve(cmd, r, c); err != nil {
			return err
		}
	}

	slog.Info(v.name, "resource", r.Name, "id", id)
	obj, err := v.call(r)(cmd, c, id, p)
	if err != nil {
		return c.explain(cmd, err)
	}
	if err := writeValue(cmd.OutOrStdout(), obj.Value, f.output); err != nil {
		return err
	}

	done := map[string]any{"Singular": r.Singular, "ID": obj.ID}
	switch v.name {
	case "create":
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("create.done", done))
	case "replace":
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("replace.done", done))
	case "patch":
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("patch.done", done))
	}
	return nil
}
