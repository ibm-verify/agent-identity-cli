package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

type getFlags struct {
	target
	output string
	query  resource.ListQuery
}

// newGet returns get, with one subcommand per resource. Like every resource
// command, it writes only data to stdout.
func newGet(opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: i18n.T("get.short", nil),
		Long:  i18n.T("get.long", nil),
	}
	for _, r := range resources() {
		cmd.AddCommand(newGetResource(opts, r))
	}
	return cmd
}

func newGetResource(opts *options, r *resource.Resource) *cobra.Command {
	var f getFlags
	cmd := &cobra.Command{
		Use:     r.Name + " [<id> | --name <name>]",
		Aliases: r.Aliases,
		Short:   i18n.T("get.resource_short", map[string]any{"Resource": r.Name}),
		Example: fmt.Sprintf("  ibm-agentid get %[1]s\n  ibm-agentid get %[1]s <id>\n  ibm-agentid get %[1]s --name <name> -o json", r.Name),
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd, opts, r, &f, args)
		},
	}

	fl := cmd.Flags()
	f.addFlag(cmd)
	fl.StringVarP(&f.output, "output", "o", "", i18n.T("get.output_flag", nil))
	// A list flag is offered only when the API has it.
	if r.ListFlags.Search {
		fl.StringVar(&f.query.Search, "search", "", i18n.T("get.search_flag", nil))
	}
	if r.ListFlags.Sort {
		fl.StringVar(&f.query.Sort, "sort", "", i18n.T("get.sort_flag", nil))
	}
	if r.ListFlags.Limit {
		fl.IntVar(&f.query.Limit, "limit", 0, i18n.T("get.limit_flag", nil))
	}
	if r.ListFlags.Page {
		fl.IntVar(&f.query.Page, "page", 0, i18n.T("get.page_flag", nil))
	}
	return cmd
}

func runGet(cmd *cobra.Command, opts *options, r *resource.Resource, f *getFlags, args []string) error {
	if err := f.parse(r, args, false); err != nil {
		return err
	}

	single := f.given()
	if single {
		for _, name := range []string{"search", "sort", "limit", "page"} {
			if fl := cmd.Flags().Lookup(name); fl != nil && fl.Changed {
				return i18n.Wrap(input.ErrInvalidInput, "resource.list_flag_with_target", map[string]any{"Flag": name})
			}
		}
	}

	format := f.output
	switch {
	case single && format == "":
		format = "yaml"
	case !single && format == "":
		format = "table"
	case single:
		if format == "table" {
			return i18n.Wrap(input.ErrInvalidInput, "resource.table_one", nil)
		}
		if err := checkFormat(format, "yaml", "json"); err != nil {
			return err
		}
	default:
		if err := checkFormat(format, "table", "yaml", "json"); err != nil {
			return err
		}
	}

	c, err := connect(cmd, opts)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()

	if single {
		id, err := f.resolve(cmd, r, c)
		if err != nil {
			return err
		}
		slog.Info("get", "resource", r.Name, "id", id)
		obj, err := r.Get(cmd.Context(), c.Conn, id)
		if err != nil {
			return c.explain(cmd, err)
		}
		return writeValue(out, obj.Value, format)
	}

	slog.Info("list", "resource", r.Name)
	l, err := r.List(cmd.Context(), c.Conn, f.query)
	if err != nil {
		return c.explain(cmd, err)
	}
	if l.Len == 0 {
		fmt.Fprintln(cmd.ErrOrStderr(), i18n.T("resource.none_found", map[string]any{"Resource": r.Name}))
	}
	if format == "table" {
		return l.WriteTable(out)
	}
	return writeValue(out, l.Items, format)
}
