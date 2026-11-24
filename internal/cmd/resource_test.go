package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
	"github.com/ibm-verify/agent-identity-cli/internal/resource/agents"
	"github.com/ibm-verify/agent-identity-cli/internal/resource/clients"
	"github.com/ibm-verify/agent-identity-cli/internal/resource/foo"
)

func init() {
	resourceList = func() []*resource.Resource {
		return []*resource.Resource{foo.Resource(), agents.Resource(), clients.Resource()}
	}
}

// stateless sets the stateless-mode variables and an empty state directory.
func stateless(t *testing.T) {
	t.Helper()
	isolate(t)
	t.Setenv("IBM_AGENTID_SERVER", "https://agentid.example.com")
	t.Setenv("IBM_AGENTID_TOKEN", "the-token")
}

// withData is run with the data written to a file and passed with -d, because
// -d - reads the real standard input, as it does for login.
func withData(t *testing.T, body string, args ...string) (string, string, error) {
	t.Helper()
	return run(t, append(args, "-d", writeData(t, body))...)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// readStore returns the file that stands in for foo's server.
func readStore(t *testing.T) (string, error) {
	t.Helper()
	dir, err := profile.Dir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(dir, "foo.json"))
	return string(b), err
}

// createFoo creates a foo from YAML and returns its ID.
func createFoo(t *testing.T, body string) string {
	t.Helper()
	out, _, err := withData(t, body, "create", "foo", "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	return m["id"].(string)
}

func TestFooLifecycle(t *testing.T) {
	stateless(t)

	out, errOut, err := withData(t, "name: one\nenabled: true\nsecret: hush\n", "create", "foo")
	if err != nil {
		t.Fatal(err)
	}
	var created map[string]any
	if err := yaml.Unmarshal([]byte(out), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)
	if id == "" || created["createdAt"] == nil {
		t.Errorf("the server should assign id and createdAt: %q", out)
	}
	if !strings.Contains(errOut, "Created foo "+id) || strings.Contains(out, "Created") {
		t.Errorf("confirmation belongs on stderr: out=%q err=%q", out, errOut)
	}
	if strings.Contains(out, "hush") || created["secret"] != resource.Mask {
		t.Errorf("secret must be masked: %q", out)
	}

	// get by id and by name give the same object.
	byID, _, err := run(t, "get", "foo", id, "-o", "json")
	if err != nil {
		t.Fatal(err)
	}
	byName, _, err := run(t, "get", "foos", "--name", "one", "-o", "json")
	if err != nil || byID != byName {
		t.Errorf("by id %q, by name %q, %v", byID, byName, err)
	}

	// patch changes only what it carries, and merges a nested object.
	if _, _, err := withData(t, "settings:\n  timeout: 5\n", "patch", "foo", id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := withData(t, "settings:\n  retries: 2\n", "patch", "foo", "--name", "one"); err != nil {
		t.Fatal(err)
	}
	out, _, _ = run(t, "get", "foo", id, "-o", "json")
	var got struct {
		Enabled  bool           `json:"enabled"`
		Settings map[string]int `json:"settings"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Enabled || got.Settings["timeout"] != 5 || got.Settings["retries"] != 2 {
		t.Errorf("patch result: %q", out)
	}

	// replace clears what it leaves out.
	if _, _, err := withData(t, "name: one\ndescription: replaced\n", "replace", "foo", id); err != nil {
		t.Fatal(err)
	}
	out, _, _ = run(t, "get", "foo", id)
	if !strings.Contains(out, "replaced") || strings.Contains(out, "enabled") || strings.Contains(out, "settings") {
		t.Errorf("replace result: %q", out)
	}

	// delete asks first. With no input the answer is no.
	_, errOut, err = run(t, "delete", "foo", id)
	if !errors.Is(err, resource.ErrNotConfirmed) || !strings.Contains(errOut, "[y/N]") {
		t.Errorf("no input: err=%v stderr=%q", err, errOut)
	}
	if _, _, err := run(t, "get", "foo", id); err != nil {
		t.Errorf("an unconfirmed delete must keep the object: %v", err)
	}
	out, errOut, err = runWithInput(t, "y\n", "delete", "foo", "--name", "one")
	if err != nil || out != "" || !strings.Contains(errOut, "Deleted foo "+id) {
		t.Errorf("confirmed: out=%q err=%q %v", out, errOut, err)
	}
	if _, _, err := run(t, "get", "foo", id); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("deleted object: %v", err)
	}
}

func TestGetOutputOfGetIsInputToReplaceAndPatch(t *testing.T) {
	stateless(t)
	id := createFoo(t, "name: rt\ntags: [a]\nsecret: hush\n")

	out, _, err := run(t, "get", "foo", id)
	if err != nil {
		t.Fatal(err)
	}
	// The id, createdAt and the masked secret are accepted and dropped.
	if _, _, err := withData(t, out, "replace", "foo", id); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if _, _, err := withData(t, out, "patch", "foo", id); err != nil {
		t.Fatalf("patch: %v", err)
	}
	// A masked secret must not overwrite the real one.
	if _, _, err := withData(t, "name: rt\nsecret: hush\n", "replace", "foo", id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := withData(t, out, "patch", "foo", id); err != nil {
		t.Fatal(err)
	}
	b, err := readStore(t)
	if err != nil || !strings.Contains(b, "hush") {
		t.Errorf("the stored secret was lost: %v %q", err, b)
	}
}

func TestGetList(t *testing.T) {
	stateless(t)

	out, errOut, err := run(t, "get", "foos")
	if err != nil || strings.TrimSpace(out) != "ID  NAME  ENABLED  TAGS" || !strings.Contains(errOut, "No foos found") {
		t.Errorf("empty: out=%q err=%q %v", out, errOut, err)
	}
	if out, _, _ := run(t, "get", "foos", "-o", "json"); strings.TrimSpace(out) != "[]" {
		t.Errorf("an empty list is [], got %q", out)
	}

	for _, n := range []string{"b-foo", "a-foo", "c-bar"} {
		createFoo(t, "name: "+n+"\n")
	}
	out, _, _ = run(t, "get", "foos")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 || strings.Fields(lines[1])[1] != "a-foo" || strings.Fields(lines[3])[1] != "c-bar" {
		t.Errorf("table: %q", out)
	}

	out, _, _ = run(t, "get", "foos", "--search", "foo", "-o", "json")
	var items []map[string]any
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 2 {
		t.Errorf("search: %q %v", out, err)
	}
	out, _, _ = run(t, "get", "foos", "--limit", "2", "--page", "2", "-o", "json")
	if err := json.Unmarshal([]byte(out), &items); err != nil || len(items) != 1 || items[0]["name"] != "c-bar" {
		t.Errorf("page 2: %q %v", out, err)
	}
	if out, _, _ := run(t, "get", "foos", "-o", "yaml"); !strings.HasPrefix(out, "- ") && !strings.HasPrefix(out, "-\n") {
		t.Errorf("yaml list: %q", out)
	}
}

func TestTargets(t *testing.T) {
	stateless(t)
	id := createFoo(t, "name: t\n")
	createFoo(t, "name: u\n")

	both := []string{"foo", id, "--name", "t"}
	for _, verb := range []string{"get", "delete"} {
		if _, _, err := run(t, append([]string{verb}, both...)...); !errors.Is(err, input.ErrInvalidInput) {
			t.Errorf("%s with both: %v", verb, err)
		}
	}
	for _, verb := range []string{"replace", "patch"} {
		if _, _, err := withData(t, "name: x\n", verb, "foo"); !errors.Is(err, input.ErrInvalidInput) {
			t.Errorf("%s with no target: %v", verb, err)
		}
	}
	if _, _, err := run(t, "delete", "foo", "--yes"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("delete with no target: %v", err)
	}
	if _, _, err := run(t, "get", "foo", id, "--search", "x"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("list flag with a target: %v", err)
	}
	if _, _, err := run(t, "get", "foo", "--name", "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("unknown name: %v", err)
	}
	if _, _, err := run(t, "get", "foo", "foo-nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	// A name is matched exactly, not as a substring.
	if _, _, err := run(t, "get", "foo", "--name", "t", "-o", "json"); err != nil {
		t.Errorf("exact name: %v", err)
	}
}

func TestPayloadChecks(t *testing.T) {
	stateless(t)
	id := createFoo(t, "name: p\n")

	cases := []struct {
		name string
		in   string
		args []string
	}{
		{"id of another object", "id: foo-other\nname: p\n", []string{"replace", "foo", id}},
		{"id of another object, patch", "id: foo-other\nname: p\n", []string{"patch", "foo", id}},
		{"nothing to change", "id: " + id + "\ncreatedAt: 2020-01-01T00:00:00Z\n", []string{"patch", "foo", id}},
		{"a list", "- a\n- b\n", []string{"create", "foo"}},
		{"a scalar", "just text\n", []string{"create", "foo"}},
		{"wrong type", "name: x\nenabled: [1]\n", []string{"create", "foo"}},
	}
	for _, c := range cases {
		out, errOut, err := withData(t, c.in, c.args...)
		if !errors.Is(err, input.ErrInvalidInput) || out != "" {
			t.Errorf("%s: out=%q err=%q %v", c.name, out, errOut, err)
		}
	}

	// A matching id is fine.
	if _, _, err := withData(t, "id: "+id+"\nname: p\n", "replace", "foo", id); err != nil {
		t.Errorf("matching id: %v", err)
	}
	// replace and create may carry an empty-looking payload to the server,
	// which has its own rules.
	if _, _, err := withData(t, "description: no name\n", "create", "foo"); err == nil || errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("the server refuses a foo without a name: %v", err)
	}
	if _, _, err := withData(t, "name: p\n", "create", "foo"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the server's message is shown as received: %v", err)
	}
}

func TestUnknownPropertiesAreIgnoredAndJSONWorks(t *testing.T) {
	stateless(t)
	out, _, err := withData(t, `{"name":"j","bogus":1,"settings":{"timeout":7}}`, "create", "foo", "-o", "json")
	if err != nil || !strings.Contains(out, `"timeout": 7`) || strings.Contains(out, "bogus") {
		t.Errorf("out=%q %v", out, err)
	}
}

func TestDataRules(t *testing.T) {
	stateless(t)

	if _, _, err := run(t, "create", "foo"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("no -d and no --template: %v", err)
	}
	// -d - on a terminal is refused by input.Read, and the test stdin is a
	// pipe, so check the file path.
	if _, _, err := run(t, "create", "foo", "-d", "/no/such/file.yaml"); !errors.Is(err, profile.ErrSourceUnavailable) {
		t.Errorf("missing file: %v", err)
	}
	if _, _, err := runWithInput(t, "name: x\n", "create", "foo", "-d", "-", "-o", "table"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("-o table on create: %v", err)
	}
	if _, _, err := run(t, "get", "foo", "x", "-o", "table"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("-o table on one object: %v", err)
	}
	if _, _, err := run(t, "get", "foos", "-o", "xml"); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("unknown format: %v", err)
	}
}

func TestBadDataFailsBeforeTheToken(t *testing.T) {
	// No token source at all: the data is judged first, so the error is about
	// the data and not about signing in.
	isolate(t)
	_, _, err := withData(t, "- a\n", "create", "foo")
	if !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("want invalid input, got %v", err)
	}
	_, _, err = withData(t, "name: x\n", "create", "foo")
	if err == nil || errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("good data and no context should fail on the context: %v", err)
	}
}

func TestTemplates(t *testing.T) {
	isolate(t) // no token needed
	for _, verb := range []string{"create", "replace", "patch"} {
		for format, marker := range map[string]string{"yaml": "# Name of the foo", "json": `"name"`} {
			out, errOut, err := run(t, verb, "foo", "--template", "-o", format)
			if err != nil || !strings.Contains(out, marker) || errOut != "" {
				t.Errorf("%s %s: out=%q err=%q %v", verb, format, out, errOut, err)
			}
		}
	}
	if _, _, err := run(t, "create", "foo", "--template", "-o", "xml"); err == nil {
		t.Error("unknown template format: want an error")
	}
}

func TestSavedContextToken(t *testing.T) {
	isolate(t)
	seed(t) // context a is current and has a valid token "at"

	if _, _, err := withData(t, "name: s\n", "create", "foo"); err != nil {
		t.Fatalf("a valid cached token should be used: %v", err)
	}

	// An expired token with no way to refresh must stop the command, not
	// reach the server.
	cfg, err := profile.Load()
	if err != nil {
		t.Fatal(err)
	}
	expired := profile.NewToken(cfg.Contexts["a"], "old", "", "Bearer", 1, time.Now().Add(-time.Hour))
	if err := profile.SaveToken("a", expired); err != nil {
		t.Fatal(err)
	}
	out, _, err := run(t, "get", "foos")
	if err == nil || out != "" || !strings.Contains(err.Error(), "ibm-agentid login") {
		t.Errorf("expired token: out=%q err=%v", out, err)
	}

	// Another context has no token at all.
	if _, _, err := run(t, "get", "foos", "--context", "b"); err == nil {
		t.Error("a context with no token should fail")
	}
	if _, _, err := run(t, "get", "foos", "--context", "nope"); !errors.Is(err, profile.ErrContextNotFound) {
		t.Errorf("unknown context: %v", err)
	}
}

func TestStatelessMode(t *testing.T) {
	isolate(t)
	seed(t) // a saved context exists, with a valid token

	t.Setenv("IBM_AGENTID_SERVER", "https://ci.example.com")
	t.Setenv("IBM_AGENTID_TOKEN", "ci-token")

	// The variables win over the saved current context, and state is untouched.
	before, _ := profile.Load()
	if _, _, err := run(t, "get", "foos"); err != nil {
		t.Fatal(err)
	}
	after, _ := profile.Load()
	if before.CurrentContext != after.CurrentContext || len(before.Contexts) != len(after.Contexts) {
		t.Error("stateless mode must not change the saved state")
	}

	// An expired saved token does not matter in stateless mode.
	cfg, _ := profile.Load()
	expired := profile.NewToken(cfg.Contexts["a"], "old", "", "Bearer", 1, time.Now().Add(-time.Hour))
	_ = profile.SaveToken("a", expired)
	if _, _, err := run(t, "get", "foos"); err != nil {
		t.Errorf("stateless mode ignores the cache: %v", err)
	}

	// --context wins: the saved context is used, and its token is expired.
	if _, _, err := run(t, "get", "foos", "--context", "a"); err == nil {
		t.Error("--context should select the saved context")
	}
}

func TestStatelessModeNeedsBothAndChecksThem(t *testing.T) {
	isolate(t)
	cases := []struct{ name, server, token, want string }{
		{"no token", "https://ci.example.com", "", "IBM_AGENTID_TOKEN"},
		{"no server", "", "ci-token", "IBM_AGENTID_SERVER"},
		{"bad server", "ftp://ci.example.com", "ci-token", "IBM_AGENTID_SERVER"},
		{"server with a password", "https://u:p@ci.example.com", "ci-token", "IBM_AGENTID_SERVER"},
		{"token with a space", "https://ci.example.com", "two words", "IBM_AGENTID_TOKEN"},
		{"token with a newline", "https://ci.example.com", "tok\nen", "IBM_AGENTID_TOKEN"},
	}
	for _, c := range cases {
		t.Setenv("IBM_AGENTID_SERVER", c.server)
		t.Setenv("IBM_AGENTID_TOKEN", c.token)
		_, errOut, err := run(t, "get", "foos")
		if !errors.Is(err, input.ErrInvalidInput) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
		if strings.Contains(err.Error(), "ci-token") || strings.Contains(errOut, "ci-token") || strings.Contains(err.Error(), "two words") {
			t.Errorf("%s: the token must never be shown: %v", c.name, err)
		}
	}

	// Nothing was created in the state directory.
	if p, _ := profile.ConfigPath(); fileExists(p) {
		t.Error("stateless mode must not create config.yaml")
	}
}

// foo's server rejects only an empty token, which the CLI never sends, so the
// hint is tested on the helper.
func TestRejectedTokenHint(t *testing.T) {
	c := &connection{stateless: true}
	err := resource.Unauthorized()
	root := NewRoot("test")
	var sink strings.Builder
	root.SetErr(&sink)
	if got := c.explain(root, err); !errors.Is(got, resource.ErrUnauthorized) || !strings.Contains(sink.String(), "IBM_AGENTID_TOKEN") {
		t.Errorf("hint: %q", sink.String())
	}
	sink.Reset()
	_ = (&connection{}).explain(root, err)
	if sink.Len() != 0 {
		t.Errorf("no hint for a saved context: %q", sink.String())
	}
}

func TestDeleteYesAndPromptText(t *testing.T) {
	stateless(t)
	id := createFoo(t, "name: d\n")

	for _, answer := range []string{"n\n", "\n", "maybe\n"} {
		if _, _, err := runWithInput(t, answer, "delete", "foo", id); !errors.Is(err, resource.ErrNotConfirmed) {
			t.Errorf("answer %q: %v", answer, err)
		}
	}
	_, errOut, err := run(t, "delete", "foo", "--name", "d", "--yes")
	if err != nil || strings.Contains(errOut, "[y/N]") || !strings.Contains(errOut, "Deleted foo "+id) {
		t.Errorf("--yes: %q %v", errOut, err)
	}
	// The prompt names what the user typed.
	createFoo(t, "name: e\n")
	_, errOut, _ = runWithInput(t, "n\n", "delete", "foo", "--name", "e")
	if !strings.Contains(errOut, `"e"`) {
		t.Errorf("prompt: %q", errOut)
	}
}

func TestHelpListsOnlyOfferedVerbs(t *testing.T) {
	isolate(t)
	for _, verb := range []string{"get", "create", "replace", "patch", "delete"} {
		out, _, err := run(t, verb, "--help")
		if err != nil || !strings.Contains(out, "foos") {
			t.Errorf("%s --help: %q %v", verb, out, err)
		}
	}
	// Aliases work under every verb.
	if _, _, err := run(t, "create", "foos", "--template"); err != nil {
		t.Errorf("plural name: %v", err)
	}
}
