package foo

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// A generated model must carry the same json and yaml name on every property,
// because input is decoded by the yaml or the json decoder depending on its
// first character. Run this test for every resource.
func TestTagsMatch(t *testing.T) {
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || typ.PkgPath() == "time" {
			return
		}
		for i := range typ.NumField() {
			f := typ.Field(i)
			j, y := f.Tag.Get("json"), f.Tag.Get("yaml")
			if j == "" || j != y {
				t.Errorf("%s.%s: json tag %q and yaml tag %q must be the same", typ.Name(), f.Name, j, y)
			}
			check(f.Type)
		}
	}
	check(reflect.TypeFor[Foo]())
}

// The template is what users start from, so it must decode in both formats
// and give the same object.
func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*Foo
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[Foo](&input.Data{Bytes: b})
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		got = append(got, v)
	}
	if !reflect.DeepEqual(got[0], got[1]) {
		a, _ := json.Marshal(got[0])
		b, _ := json.Marshal(got[1])
		t.Errorf("yaml and json differ:\n%s\n%s", a, b)
	}
	if got[0].Name == nil || got[0].Settings == nil || got[0].Settings.Timeout == nil || got[0].Tags == nil {
		t.Errorf("the template should show every kind of property: %+v", got[0])
	}
	if !strings.Contains(string(mustTemplate(t, r)), "# ") {
		t.Error("the yaml template should explain each property")
	}
}

func mustTemplate(t *testing.T, r *resource.Resource) []byte {
	t.Helper()
	b, err := r.Template("yaml")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestResourceOffersEveryVerb(t *testing.T) {
	r := Resource()
	if r.Get == nil || r.List == nil || r.Find == nil || r.Create == nil || r.Replace == nil || r.Patch == nil || r.Delete == nil {
		t.Errorf("foo has every verb: %+v", r)
	}
	if r.Name != "foos" || !reflect.DeepEqual(r.Aliases, []string{"foo"}) {
		t.Errorf("names: %q %v", r.Name, r.Aliases)
	}
}

// What get prints is accepted as input: the read-only properties and an
// untouched (masked) secret are dropped, and a changed secret is kept. The
// check is on what reaches the store.
func TestStripAndMask(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r := Resource()
	conn := resource.Conn{Token: "t"}

	for i, c := range []struct{ secret, stored string }{{"'" + resource.Mask + "'", ""}, {"new", "new"}} {
		name := "n" + string(rune('a'+i))
		p, err := r.Parse(&input.Data{Bytes: []byte("id: foo-1\nname: " + name + "\ncreatedAt: 2020-01-01T00:00:00Z\nsecret: " + c.secret + "\n")})
		if err != nil {
			t.Fatal(err)
		}
		if p.Empty() {
			t.Fatal("the name is left, so the payload is not empty")
		}
		obj, err := r.Create(t.Context(), conn, p)
		if err != nil {
			t.Fatal(err)
		}
		if obj.ID == "foo-1" {
			t.Error("the server must assign the id, not take it from the data")
		}
		stored, err := get(t.Context(), conn, obj.ID)
		if err != nil {
			t.Fatal(err)
		}
		if str(stored.Secret) != c.stored {
			t.Errorf("secret %q: stored %q, want %q", c.secret, str(stored.Secret), c.stored)
		}
		if shown := obj.Value.(*Foo).Secret; c.stored != "" && str(shown) != resource.Mask {
			t.Errorf("the output must be masked: %q", str(shown))
		}
	}

	only, err := r.Parse(&input.Data{Bytes: []byte("id: foo-1\ncreatedAt: 2020-01-01T00:00:00Z\n")})
	if err != nil || !only.Empty() {
		t.Errorf("read-only properties only should be empty: %v", err)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	r := Resource()
	if _, err := r.List(t.Context(), resource.Conn{}, resource.ListQuery{}); !errors.Is(err, resource.ErrUnauthorized) {
		t.Errorf("no token: %v", err)
	}
	if err := r.Delete(t.Context(), resource.Conn{}, "x"); !errors.Is(err, resource.ErrUnauthorized) {
		t.Errorf("no token: %v", err)
	}
}
