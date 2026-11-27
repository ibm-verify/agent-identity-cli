package rules

import (
	"strconv"

	"github.com/ibm-verify/agent-identity-cli/internal/client/platform"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// Rule is the payload of the rules resource. The API lists rules as summaries
// without their source and reads one as a full rule, so this joins the two:
// the source is left out of a list instead of being printed empty, and what
// get prints can be sent back to replace.
type Rule struct {
	// Name is the unique name, the identifier.
	Name string `json:"name" yaml:"name"`
	// Class is accesspolicy or mappingrule.
	Class platform.RuleClass `json:"class" yaml:"class"`
	// Source is the JavaScript source. It is not part of a list.
	Source string `json:"source,omitempty" yaml:"source,omitempty"`

	// Type, ImportScriptNames and ImportBindingNames are derived by the server.
	Type               string   `json:"type,omitempty" yaml:"type,omitempty"`
	ImportScriptNames  []string `json:"import_script_names,omitempty" yaml:"import_script_names,omitempty"`
	ImportBindingNames []string `json:"import_binding_names,omitempty" yaml:"import_binding_names,omitempty"`

	// CreatedAt and UpdatedAt are Unix timestamps in milliseconds, set by the server.
	CreatedAt *int64 `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt *int64 `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

const template = `# Unique name of the rule. Identifier, immutable. Required.
# A mapping rule can call another one with importMappingRule("<name>").
name: agent-cc-premapping
# Class of the rule: mappingrule or accesspolicy. Required.
class: mappingrule
# JavaScript source of the rule. Required.
# The language, the imported rules and classes, and the compiled form are
# derived by the server. The server does not check the syntax on write.
source: |
  importClass(Packages.com.tivoli.am.fim.trustserver.sts.utilities.IDMappingExtUtils);
  IDMappingExtUtils.traceString("client_credentials pre-mapping rule");
  tokenData["custom_claim"] = "agent-flow";
  idtokenData["custom_claim"] = "agent-flow";
`

// timestamp formats a millisecond Unix timestamp for a table cell.
func timestamp(t *int64) string {
	if t == nil {
		return ""
	}
	return strconv.FormatInt(*t, 10)
}

// Resource returns the definition of rules, which is all that cmd.NewRoot
// needs. The API has no PATCH, so the patch verb is not offered. The name is
// both the ID and the name.
func Resource() *resource.Resource {
	d := &resource.Def[Rule]{
		Name:     "rules",
		Singular: "rule",
		Aliases:  []string{"rule"},
		Template: template,
		Columns: []resource.Column[Rule]{
			{Header: "CLASS", Value: func(r *Rule) string { return string(r.Class) }},
			{Header: "UPDATED_AT", Value: func(r *Rule) string { return timestamp(r.UpdatedAt) }},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(r *Rule) string { return r.Name },
		NameOf: func(r *Rule) string { return r.Name },

		// The server derives the type and the imports and assigns the
		// timestamps, so input that carries them (the output of get) is
		// accepted and they are dropped.
		Strip: func(r *Rule) {
			r.Type, r.ImportScriptNames, r.ImportBindingNames = "", nil, nil
			r.CreatedAt, r.UpdatedAt = nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
