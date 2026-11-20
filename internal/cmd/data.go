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

func (d *dataFlags) add(cmd *cobra.Command) {
	f := cmd.Flags()
	f.StringVarP(&d.file, "data", "d", "", i18n.T("data.data_flag", nil))
	f.BoolVar(&d.template, "template", false, i18n.T("data.template_flag", nil))
	f.StringVarP(&d.output, "output", "o", "yaml", i18n.T("data.output_flag", nil))
}
