package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
	"github.com/ibm-verify/agent-identity-cli/internal/resource/agents"
	"github.com/ibm-verify/agent-identity-cli/internal/resource/clients"
)

// resourceList is the list of active resources offered by the CLI.
var resourceList = func() []*resource.Resource {
	return []*resource.Resource{agents.Resource(), clients.Resource()}
}

// resources is every resource the verbs offer, in the order shown in help.
// Add a resource here.
func resources() []*resource.Resource {
	return resourceList()
}

// target is the object a command acts on: an ID argument or --name.
type target struct {
	id   string
	name string
}

func (t *target) addFlag(cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.name, "name", "", i18n.T("resource.name_flag", nil))
}

// parse takes the ID from args and checks that no more than one way of naming
// the object is used. With required, naming none is an error too.
func (t *target) parse(r *resource.Resource, args []string, required bool) error {
	if len(args) == 1 {
		t.id = args[0]
	}
	switch {
	case t.id != "" && t.name != "":
		return i18n.Wrap(input.ErrInvalidInput, "resource.target_both", nil)
	case required && t.id == "" && t.name == "":
		return i18n.Wrap(input.ErrInvalidInput, "resource.target_required", map[string]any{"Singular": r.Singular})
	}
	return nil
}

func (t *target) given() bool { return t.id != "" || t.name != "" }

// resolve returns the ID, looking the name up when --name was used.
func (t *target) resolve(cmd *cobra.Command, r *resource.Resource, c *connection) (string, error) {
	if t.name == "" {
		return t.id, nil
	}
	id, err := r.Find(cmd.Context(), c.Conn, t.name)
	return id, c.explain(cmd, err)
}

// checkFormat fails unless format is one of allowed.
func checkFormat(format string, allowed ...string) error {
	if slices.Contains(allowed, format) {
		return nil
	}
	return i18n.Wrap(input.ErrInvalidInput, "resource.unknown_output", map[string]any{
		"Format": format, "Allowed": strings.Join(allowed, ", "),
	})
}

// writeValue prints v as yaml or json.
func writeValue(w io.Writer, v any, format string) error {
	if format == "json" {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	}
	return writeYAML(w, v)
}
