// Package resource defines what the resource commands (get, create, replace,
// patch and delete) need to know about one kind of server object, so that
// adding an object adds one definition and no command code. See spec.md.
package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
)

// Sentinel errors for matching with errors.Is. Input errors use
// input.ErrInvalidInput.
var (
	ErrNotFound      = errors.New("not found")
	ErrAmbiguousName = errors.New("ambiguous name")
	ErrNotConfirmed  = errors.New("not confirmed")
	ErrUnauthorized  = errors.New("unauthorized")
)

// Mask replaces a secret in output.
const Mask = "********"

// NotFound is the error for a target that does not exist. A definition
// returns it when the server answers 404.
func NotFound(singular, target string) error {
	return i18n.Wrap(ErrNotFound, "resource.not_found", map[string]any{"Singular": singular, "Target": target})
}

// Unauthorized is the error for a token the server rejected. A definition
// returns it when the server answers 401.
func Unauthorized() error {
	return i18n.Wrap(ErrUnauthorized, "resource.unauthorized", nil)
}

// FirstError returns the first of the error payloads that is not nil, or nil.
// A generated client has a typed payload for each status code.
func FirstError[T any](errs ...*T) *T {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// Conn is what a definition needs to call the server: the base URL, a bearer
// token that the command layer has already made valid, and an HTTP client that
// logs and applies the context's TLS settings.
type Conn struct {
	Server string
	Token  string
	HTTP   *http.Client
}

// ListQuery is a list request. A field the definition does not support (see
// ListSupport) is never set.
type ListQuery struct {
	Search string
	Sort   string
	Limit  int
	Page   int
}

// ListSupport says which list flags the API has.
type ListSupport struct {
	Limit, Page, Sort, Search bool
}

// Column is a table column. The table always starts with ID and NAME. Headers
// are English data, like templates.
type Column[T any] struct {
	Header string
	Value  func(*T) string
}

// Def describes one resource over its payload type T, normally a generated
// model. Get and List are required. A verb whose function is nil is not offered.
type Def[T any] struct {
	// Name is the plural command name. Singular is used in messages.
	Name     string
	Singular string
	Aliases  []string

	// Template is the YAML skeleton printed by --template, with a comment per
	// property.
	Template string
	Columns  []Column[T]

	ListFlags ListSupport

	// ID and NameOf read the identifier and the unique name.
	ID     func(*T) string
	NameOf func(*T) string

	// Strip clears the read-only properties, so that the output of get can be
	// sent back. Mask hides secrets in output. Either may be nil.
	Strip func(*T)
	Mask  func(*T)

	Get     func(ctx context.Context, c Conn, id string) (*T, error)
	List    func(ctx context.Context, c Conn, q ListQuery) ([]*T, error)
	Create  func(ctx context.Context, c Conn, v *T) (*T, error)
	Replace func(ctx context.Context, c Conn, id string, v *T) (*T, error)
	Patch   func(ctx context.Context, c Conn, id string, v *T) (*T, error)
	Delete  func(ctx context.Context, c Conn, id string) error
}

// Object is one resource, ready to print. Value is already masked.
type Object struct {
	Value any
	ID    string
	Name  string
}

// Payload is parsed -d data, ready to send. It is made by Resource.Parse,
// which needs no connection, so bad data fails before any token is fetched.
type Payload struct {
	v     any // *T
	id    string
	empty bool
}

// Empty reports whether nothing is left after the read-only properties are
// dropped.
func (p *Payload) Empty() bool { return p.empty }

// Resource is a Def with the payload type hidden, which is what the commands
// use. Build it with Def.Resource. A verb function is nil when the definition
// has none.
type Resource struct {
	Name      string
	Singular  string
	Aliases   []string
	ListFlags ListSupport

	Template func(format string) ([]byte, error)
	Parse    func(d *input.Data) (*Payload, error)

	Get  func(ctx context.Context, c Conn, id string) (*Object, error)
	List func(ctx context.Context, c Conn, q ListQuery) (*Listing, error)
	// Find returns the id of the one object with exactly this name.
	Find func(ctx context.Context, c Conn, name string) (id string, err error)

	Create  func(ctx context.Context, c Conn, p *Payload) (*Object, error)
	Replace func(ctx context.Context, c Conn, id string, p *Payload) (*Object, error)
	Patch   func(ctx context.Context, c Conn, id string, p *Payload) (*Object, error)
	Delete  func(ctx context.Context, c Conn, id string) error
}

// Resource builds the command-facing form of the definition.
func (d *Def[T]) Resource() *Resource {
	r := &Resource{Name: d.Name, Singular: d.Singular, Aliases: d.Aliases, ListFlags: d.ListFlags}

	r.Template = func(format string) ([]byte, error) { return input.TemplateText(d.Template, format) }

	// Parse reads the payload. Read-only properties are dropped, so the output
	// of get can be sent back, but the ID is remembered to check the target.
	r.Parse = func(data *input.Data) (*Payload, error) {
		v, err := input.Decode[T](data)
		if err != nil {
			return nil, err
		}
		p := &Payload{v: v, id: d.ID(v)}
		if d.Strip != nil {
			d.Strip(v)
		}
		p.empty = reflect.ValueOf(v).Elem().IsZero()
		return p, nil
	}

	object := func(v *T) *Object {
		if d.Mask != nil {
			d.Mask(v)
		}
		return &Object{Value: v, ID: d.ID(v), Name: d.NameOf(v)}
	}

	r.Get = func(ctx context.Context, c Conn, id string) (*Object, error) {
		v, err := d.Get(ctx, c, id)
		if err != nil {
			return nil, err
		}
		return object(v), nil
	}

	r.Find = func(ctx context.Context, c Conn, name string) (string, error) {
		items, err := d.List(ctx, c, ListQuery{Search: name})
		if err != nil {
			return "", err
		}
		var ids []string
		for _, v := range items {
			if d.NameOf(v) == name {
				ids = append(ids, d.ID(v))
			}
		}
		switch len(ids) {
		case 0:
			return "", NotFound(d.Singular, name)
		case 1:
			return ids[0], nil
		}
		return "", i18n.Wrap(ErrAmbiguousName, "resource.ambiguous_name", map[string]any{
			"Singular": d.Singular, "Name": name, "IDs": strings.Join(ids, ", "),
		})
	}

	r.List = func(ctx context.Context, c Conn, q ListQuery) (*Listing, error) {
		items, err := d.List(ctx, c, q)
		if err != nil {
			return nil, err
		}
		if items == nil {
			items = []*T{}
		}
		for _, v := range items {
			if d.Mask != nil {
				d.Mask(v)
			}
		}
		return &Listing{Items: items, Len: len(items), headers: d.headers(), rows: func() [][]string { return d.rows(items) }}, nil
	}

	// A payload that names another object is refused, so that a copied file
	// cannot overwrite the wrong one. The name is not checked: it can change.
	sameID := func(p *Payload, id string) error {
		if p.id != "" && p.id != id {
			return i18n.Wrap(input.ErrInvalidInput, "resource.id_mismatch", nil)
		}
		return nil
	}

	if d.Create != nil {
		r.Create = func(ctx context.Context, c Conn, p *Payload) (*Object, error) {
			out, err := d.Create(ctx, c, p.v.(*T))
			if err != nil {
				return nil, err
			}
			return object(out), nil
		}
	}

	if d.Replace != nil {
		r.Replace = func(ctx context.Context, c Conn, id string, p *Payload) (*Object, error) {
			if err := sameID(p, id); err != nil {
				return nil, err
			}
			out, err := d.Replace(ctx, c, id, p.v.(*T))
			if err != nil {
				return nil, err
			}
			return object(out), nil
		}
	}

	if d.Patch != nil {
		r.Patch = func(ctx context.Context, c Conn, id string, p *Payload) (*Object, error) {
			if err := sameID(p, id); err != nil {
				return nil, err
			}
			out, err := d.Patch(ctx, c, id, p.v.(*T))
			if err != nil {
				return nil, err
			}
			return object(out), nil
		}
	}

	r.Delete = d.Delete
	return r
}

func (d *Def[T]) headers() []string {
	h := []string{"ID", "NAME"}
	for _, c := range d.Columns {
		h = append(h, c.Header)
	}
	return h
}

func (d *Def[T]) rows(items []*T) [][]string {
	rows := make([][]string, 0, len(items))
	for _, v := range items {
		row := []string{cell(d.ID(v)), cell(d.NameOf(v))}
		for _, c := range d.Columns {
			row = append(row, cell(c.Value(v)))
		}
		rows = append(rows, row)
	}
	return rows
}

// cell shows an empty value as "-", so that columns stay countable.
func cell(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Join joins a list for a table cell.
func Join(items []string) string { return strings.Join(items, ",") }

// Str returns the value of an optional string, or "".
func Str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Bool returns an optional bool as "true" or "false", or "" when unset.
func Bool(p *bool) string {
	if p == nil {
		return ""
	}
	return fmt.Sprint(*p)
}
