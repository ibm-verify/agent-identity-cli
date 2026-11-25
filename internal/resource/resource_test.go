package resource

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ibm-verify/agent-identity-cli/internal/input"
)

type thing struct {
	ID   *string `json:"id,omitempty" yaml:"id,omitempty"`
	Name *string `json:"name,omitempty" yaml:"name,omitempty"`
}

func ptr(s string) *string { return &s }

// fake is a definition whose list returns whatever the test sets.
func fake(items ...*thing) *Resource {
	d := &Def[thing]{
		Name: "things", Singular: "thing",
		Template: "name: x\n",
		ID:       func(v *thing) string { return Str(v.ID) },
		NameOf:   func(v *thing) string { return Str(v.Name) },
		Get:      func(context.Context, Conn, string) (*thing, error) { return items[0], nil },
		List:     func(context.Context, Conn, ListQuery) ([]*thing, error) { return items, nil },
	}
	return d.Resource()
}

func TestFindMatchesTheNameExactly(t *testing.T) {
	r := fake(
		&thing{ID: ptr("1"), Name: ptr("web")},
		&thing{ID: ptr("2"), Name: ptr("web-2")}, // the server's search is a substring match
	)
	id, err := r.Find(t.Context(), Conn{}, "web")
	if err != nil || id != "1" {
		t.Errorf("id=%q err=%v", id, err)
	}
	if _, err := r.Find(t.Context(), Conn{}, "we"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a prefix is not a match: %v", err)
	}
}

func TestFindRefusesToPickOne(t *testing.T) {
	r := fake(&thing{ID: ptr("1"), Name: ptr("dup")}, &thing{ID: ptr("2"), Name: ptr("dup")})
	_, err := r.Find(t.Context(), Conn{}, "dup")
	if !errors.Is(err, ErrAmbiguousName) || !strings.Contains(err.Error(), "1, 2") {
		t.Errorf("want an ambiguity error listing both ids, got %v", err)
	}
}

func TestOptionalVerbsAreNilWhenTheDefinitionHasNone(t *testing.T) {
	r := fake(&thing{ID: ptr("1"), Name: ptr("a")})
	if r.Create != nil || r.Replace != nil || r.Patch != nil || r.Delete != nil {
		t.Errorf("a verb without a function must not be offered: %+v", r)
	}
	if r.Get == nil || r.List == nil || r.Find == nil {
		t.Error("get, list and find are always offered")
	}
}

func TestListingTable(t *testing.T) {
	r := fake(&thing{ID: ptr("1"), Name: ptr("a")}, &thing{ID: ptr("2")})
	l, err := r.List(t.Context(), Conn{}, ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := l.WriteTable(&b); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if len(lines) != 3 || strings.Fields(lines[0])[1] != "NAME" || strings.Fields(lines[2])[1] != "-" {
		t.Errorf("table: %q", b.String())
	}
}

func TestParseKeepsIDOnlyForTheCheck(t *testing.T) {
	r := fake(&thing{})
	p, err := r.Parse(&input.Data{Bytes: []byte("name: n\n")})
	if err != nil || p.Empty() {
		t.Fatalf("%v empty=%v", err, p.Empty())
	}
	if _, err := r.Parse(&input.Data{Bytes: []byte("[1]")}); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("a list: %v", err)
	}
}

func TestFirstError(t *testing.T) {
	type payload struct{ n int }
	a, b := &payload{1}, &payload{2}
	if FirstError(nil, a, b) != a || FirstError[payload](nil, nil) != nil || FirstError[payload]() != nil {
		t.Error("FirstError should return the first non-nil payload")
	}
}
