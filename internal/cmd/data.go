package cmd

import (
	"github.com/spf13/cobra"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

// dataFlags are the flags every command that takes a data payload has, so that
// -d and --template behave the same everywhere.
type dataFlags struct {
	file     string
	template bool
	output   string
}

// addInput adds -d and --template. A command that formats its own output adds
// its own -o.
func (d *dataFlags) addInput(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&d.file, "data", "d", "", i18n.T("data.data_flag", nil))
	f.BoolVar(&d.template, "template", false, i18n.T("data.template_flag", nil))
}

// add adds -d, --template and an -o that sets the format of --template.
func (d *dataFlags) add(cmd *cobra.Command) {
	d.addInput(cmd)
	cmd.Flags().StringVarP(&d.output, "output", "o", "yaml", i18n.T("data.output_flag", nil))
}
