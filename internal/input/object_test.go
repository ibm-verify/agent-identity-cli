package input

import (
	"errors"
	"strings"
	"testing"
)

type sample struct {
	Name     *string `json:"name,omitempty" yaml:"name,omitempty"`
	Enabled  *bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	Nested   *inner  `json:"nested,omitempty" yaml:"nested,omitempty"`
	Optional *string `json:"optional,omitempty" yaml:"optional,omitempty"`
}

type inner struct {
	Count *int `json:"count,omitempty" yaml:"count,omitempty"`
}

func TestDecodeYAMLAndJSONGiveTheSameObject(t *testing.T) {
	y, err := Decode[sample](&Data{Bytes: []byte("name: a\nenabled: true\nnested:\n  count: 3\n")})
	if err != nil {
		t.Fatal(err)
	}
	j, err := Decode[sample](&Data{Bytes: []byte(`{"name":"a","enabled":true,"nested":{"count":3}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if *y.Name != *j.Name || *y.Enabled != *j.Enabled || *y.Nested.Count != *j.Nested.Count || *y.Nested.Count != 3 {
		t.Errorf("yaml %+v, json %+v", y, j)
	}
	// An absent property stays nil, which is what makes patch send only what
	// the user wrote.
	if y.Optional != nil || j.Optional != nil {
		t.Error("an absent property must stay nil")
	}
}

func TestDecodeIgnoresUnknownAndRejectsWrongShapes(t *testing.T) {
	if _, err := Decode[sample](&Data{Bytes: []byte("name: a\nbogus: 1\n")}); err != nil {
		t.Errorf("unknown property: %v", err)
	}
	for name, in := range map[string]string{
		"list":       "- a\n- b\n",
		"scalar":     "text\n",
		"wrong type": "enabled: [1]\n",
		"bad json":   `{"name":`,
	} {
		_, err := Decode[sample](&Data{Bytes: []byte(in)})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s: %v", name, err)
		}
		if err != nil && strings.Contains(err.Error(), "bogus") {
			t.Errorf("%s: the error must not quote the data", name)
		}
	}
}

func TestTemplateText(t *testing.T) {
	const y = "# a comment\nname: a\nnested:\n  count: 2\n"
	if b, err := TemplateText(y, ""); err != nil || string(b) != y {
		t.Errorf("default is yaml as written: %q %v", b, err)
	}
	b, err := TemplateText(y, "json")
	if err != nil || strings.Contains(string(b), "comment") || !strings.Contains(string(b), `"count": 2`) {
		t.Errorf("json: %q %v", b, err)
	}
	if _, err := TemplateText(y, "xml"); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("unknown format: %v", err)
	}
}

func TestValidServer(t *testing.T) {
	for s, want := range map[string]bool{
		"https://a.example.com":      true,
		"http://localhost:8080/base": true,
		"ftp://a.example.com":        false,
		"https://":                   false,
		"https://u:p@a.example.com":  false,
		"a.example.com":              false,
		"":                           false,
	} {
		if got := ValidServer(s); got != want {
			t.Errorf("ValidServer(%q) = %v", s, got)
		}
	}
}
