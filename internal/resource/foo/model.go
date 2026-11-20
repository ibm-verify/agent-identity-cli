// Package foo is a stand-in resource. It shows how to add one: a model, the
// calls to the server, and a definition. The "server" is a JSON file in the
// state directory, so every verb can be tried without a real service. Remove
// the package, and its entry in cmd.NewRoot, when real resources exist.
package foo

import (
	"time"
)

// Foo stands in for a generated model. A generated model has the same shape:
// optional properties are pointers with omitempty, and the json and yaml names
// are the same (see the struct-tags option of oapi-codegen). Pointers matter
// for patch: a property that is nil is not sent, so it is left as it is.
type Foo struct {
	ID          *string      `json:"id,omitempty" yaml:"id,omitempty"`
	Name        *string      `json:"name,omitempty" yaml:"name,omitempty"`
	Description *string      `json:"description,omitempty" yaml:"description,omitempty"`
	Enabled     *bool        `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Tags        *[]string    `json:"tags,omitempty" yaml:"tags,omitempty"`
	Settings    *FooSettings `json:"settings,omitempty" yaml:"settings,omitempty"`
	Secret      *string      `json:"secret,omitempty" yaml:"secret,omitempty"`
	CreatedAt   *time.Time   `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
}

// FooSettings is a nested object, to show how patch treats one.
type FooSettings struct {
	Timeout *int `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries *int `json:"retries,omitempty" yaml:"retries,omitempty"`
}
