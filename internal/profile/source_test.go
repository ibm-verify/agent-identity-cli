package profile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSourceInlineAndFile(t *testing.T) {
	if Source("hunter2").IsFile() || Source("hunter2").Path() != "" {
		t.Error("a plain value is inline")
	}
	if !Source("@/x/y").IsFile() || Source("@/x/y").Path() != "/x/y" {
		t.Error("@ marks a file reference")
	}
	if Source("a@b").IsFile() {
		t.Error("only a leading @ marks a file")
	}
	if Source("").IsFile() {
		t.Error("empty is not a file")
	}
}

func TestSourceRead(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "secret", "from-file\n")

	got, err := Source("@" + path).Read()
	if err != nil || string(got) != "from-file\n" {
		t.Errorf("file: %q, %v", got, err)
	}
	got, err = Source("inline-value").Read()
	if err != nil || string(got) != "inline-value" {
		t.Errorf("inline: %q, %v", got, err)
	}
	if got, err := Source("").Read(); err != nil || len(got) != 0 {
		t.Errorf("empty: %q, %v", got, err)
	}
}

func TestSourceReadErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	_, err := Source("@" + missing).Read()
	if !errors.Is(err, ErrSourceUnavailable) {
		t.Fatalf("err = %v, want ErrSourceUnavailable", err)
	}
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("message should name the path: %q", err)
	}
	if _, err := Source("@").Read(); !errors.Is(err, ErrSourceUnavailable) {
		t.Errorf("bare @: err = %v", err)
	}
}

func TestSourceResolve(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "key.json", "{}")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	want := Source("@" + filepath.Join(dir, "key.json"))

	t.Run("relative to the base directory", func(t *testing.T) {
		if got, err := Source("@./key.json").Resolve(dir); err != nil || got != want {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("parent segments are cleaned", func(t *testing.T) {
		if got, err := Source("@../key.json").Resolve(sub); err != nil || got != want {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("absolute path ignores the base", func(t *testing.T) {
		if got, err := want.Resolve(sub); err != nil || got != want {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("empty base uses the working directory", func(t *testing.T) {
		t.Chdir(dir)
		got, err := Source("@key.json").Resolve("")
		if err != nil || !filepath.IsAbs(got.Path()) {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("inline is unchanged", func(t *testing.T) {
		if got, err := Source("not-a-file").Resolve(dir); err != nil || got != "not-a-file" {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("empty is unchanged", func(t *testing.T) {
		if got, err := Source("").Resolve(dir); err != nil || got != "" {
			t.Errorf("got %q, %v", got, err)
		}
	})
}

func TestSourceResolveRejects(t *testing.T) {
	dir := t.TempDir()
	for name, in := range map[string]Source{
		"missing":   "@./absent",
		"directory": "@.",
		"bare @":    "@",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := in.Resolve(dir); !errors.Is(err, ErrSourceUnavailable) {
				t.Errorf("err = %v, want ErrSourceUnavailable", err)
			}
		})
	}
}

func TestSourceResolveDoesNotReadTheFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "locked", "secret")
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Source("@" + path).Resolve(dir); err != nil {
		t.Errorf("resolving checks existence only: %v", err)
	}
}

func TestSourceIsPrintedAsWritten(t *testing.T) {
	if got := fmt.Sprintf("%v %s", Source("-----BEGIN CERTIFICATE-----"), Source("@/ca.pem")); got != "-----BEGIN CERTIFICATE----- @/ca.pem" {
		t.Errorf("a non-secret Source prints as written: %q", got)
	}
}

func TestSecretSourceHidesInlineValues(t *testing.T) {
	cases := map[SecretSource]string{
		"":               "",
		"@/etc/key.json": "@/etc/key.json",
		"hunter2":        "(inline)",
	}
	for in, want := range cases {
		if got := in.String(); got != want {
			t.Errorf("String(%q) = %q, want %q", string(in), got, want)
		}
	}
	out := fmt.Sprintf("%v %s %+v %q", SecretSource("hunter2"), SecretSource("hunter2"), SecretSource("hunter2"), SecretSource("hunter2"))
	if strings.Contains(out, "hunter2") {
		t.Errorf("formatted output leaks the value: %q", out)
	}
}

func TestSecretSourceBehavesLikeSource(t *testing.T) {
	dir := t.TempDir()
	path := writeTemp(t, dir, "s", "from-file")

	got, err := SecretSource("@./s").Resolve(dir)
	if err != nil || got != SecretSource("@"+path) {
		t.Fatalf("Resolve = %q, %v", got, err)
	}
	data, err := got.Read()
	if err != nil || string(data) != "from-file" {
		t.Errorf("Read = %q, %v", data, err)
	}
	if data, _ := SecretSource("inline").Read(); string(data) != "inline" {
		t.Errorf("inline Read = %q", data)
	}
	if !got.IsFile() || got.Path() != path {
		t.Errorf("IsFile/Path = %v %q", got.IsFile(), got.Path())
	}
}

func TestSourceTypesSaveAsPlainStrings(t *testing.T) {
	type doc struct {
		CA     Source       `yaml:"ca,omitempty" json:"ca,omitempty"`
		Key    SecretSource `yaml:"key,omitempty" json:"key,omitempty"`
		Secret SecretSource `yaml:"secret,omitempty" json:"secret,omitempty"`
	}
	in := doc{CA: "@/abs/ca.pem", Key: "@/abs/key.json", Secret: "hunter2"}

	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out doc
	if err := yaml.Unmarshal(data, &out); err != nil || out != in {
		t.Errorf("round trip = %+v, %v\n%s", out, err, data)
	}
	if !strings.Contains(string(data), "hunter2") {
		t.Errorf("an inline value is saved as written: %s", data)
	}
}
