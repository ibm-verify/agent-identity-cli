package foo

import (
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Name of the foo. It is unique.
name: example
# What the foo is for.
description: An example foo.
# Whether the foo is in use.
enabled: true
# Free-form labels. A patch replaces the whole list.
tags: [demo, sample]
# Nested settings. A patch changes only the properties given.
settings:
  # Timeout in seconds.
  timeout: 30
  # Number of retries.
  retries: 3
# A secret. It is never shown again.
secret: change-me
`

// Resource returns the definition of foo, which is all that cmd.NewRoot needs.
// To add another resource, copy this function: describe the model, say which
// verbs and list flags the API has, and name the read-only and secret
// properties.
func Resource() *resource.Resource {
	d := &resource.Def[Foo]{
		Name:     "foos",
		Singular: "foo",
		Aliases:  []string{"foo"},
		Template: template,
		Columns: []resource.Column[Foo]{
			{Header: "ENABLED", Value: func(f *Foo) string { return resource.Bool(f.Enabled) }},
			{Header: "TAGS", Value: func(f *Foo) string {
				if f.Tags == nil {
					return ""
				}
				return resource.Join(*f.Tags)
			}},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(f *Foo) string { return resource.Str(f.ID) },
		NameOf: func(f *Foo) string { return resource.Str(f.Name) },

		// The server assigns these, so input that carries them (the output of
		// get) is accepted and they are dropped. So is a secret that is still
		// the mask, because get hides it and the file was not edited.
		Strip: func(f *Foo) {
			f.ID, f.CreatedAt = nil, nil
			if f.Secret != nil && *f.Secret == resource.Mask {
				f.Secret = nil
			}
		},
		Mask: func(f *Foo) {
			if f.Secret != nil && *f.Secret != "" {
				m := resource.Mask
				f.Secret = &m
			}
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Patch:   patch,
		Delete:  remove,
	}
	return d.Resource()
}
