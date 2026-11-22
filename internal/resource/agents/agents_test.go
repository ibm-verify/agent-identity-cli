package agents

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	seen := map[reflect.Type]bool{}
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || typ.PkgPath() == "time" || seen[typ] {
			return
		}
		seen[typ] = true
		for i := range typ.NumField() {
			f := typ.Field(i)
			if !f.IsExported() {
				continue
			}
			j, y := f.Tag.Get("json"), f.Tag.Get("yaml")
			if j == "" || j != y {
				t.Errorf("%s.%s: json tag %q and yaml tag %q must be the same", typ.Name(), f.Name, j, y)
			}
			check(f.Type)
		}
	}
	for _, typ := range []reflect.Type{
		reflect.TypeFor[Agent](),
		reflect.TypeFor[AgentListItem](),
		reflect.TypeFor[AgentListResponse](),
		reflect.TypeFor[CountResponse](),
		reflect.TypeFor[CreateAgentRequest](),
		reflect.TypeFor[UpdateAgentRequest](),
		reflect.TypeFor[PatchRequest](),
		reflect.TypeFor[SCIMError](),
	} {
		check(typ)
	}
}

// The template is what users start from, so it must decode in both formats
// and give the same object.
func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*Agent
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[Agent](&input.Data{Bytes: b})
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
	a := got[0]
	if a.DisplayName == "" || a.Description == nil || a.Status == nil || a.Tags == nil || a.ExternalIds == nil || a.Owners == nil || len(*a.Owners) == 0 {
		t.Errorf("the template should show every kind of property: %+v", a)
	}
	b, err := r.Template("yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# ") {
		t.Error("the yaml template should explain each property")
	}
}

func TestResourceOffersEveryVerb(t *testing.T) {
	r := Resource()
	if r.Get == nil || r.List == nil || r.Find == nil || r.Create == nil || r.Replace == nil || r.Patch == nil || r.Delete == nil {
		t.Errorf("agents has every verb: %+v", r)
	}
	if r.Name != "agents" || r.Singular != "agent" || !reflect.DeepEqual(r.Aliases, []string{"agent"}) {
		t.Errorf("names: %q %q %v", r.Name, r.Singular, r.Aliases)
	}
}

// call is one request the mock server received.
type call struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   map[string]any
}

// mock starts a server that records requests and answers each with h's status
// and body. The Content-Type is SCIM, as the real server's.
func mock(t *testing.T, h func(c call) (int, string)) (resource.Conn, *[]call) {
	t.Helper()
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Auth: r.Header.Get("Authorization")}
		if b, _ := io.ReadAll(r.Body); len(b) > 0 {
			if err := json.Unmarshal(b, &c.Body); err != nil {
				t.Errorf("request body is not json: %v: %s", err, b)
			}
		}
		calls = append(calls, c)
		status, body := h(c)
		w.Header().Set("Content-Type", "application/scim+json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return resource.Conn{Server: srv.URL, Token: "tok", HTTP: srv.Client()}, &calls
}

func scimError(status, detail string) string {
	return `{"schemas":["urn:ietf:params:scim:api:messages:2.0:Error"],"status":"` + status + `","detail":"` + detail + `"}`
}

const agentJSON = `{"schemas":["urn:ietf:params:scim:schemas:core:ibm:2.0:Agent"],"id":"a-1","displayName":"alpha","description":"d","status":"ACTIVE","tags":["x","y"]}`

func parse(t *testing.T, r *resource.Resource, text string) *resource.Payload {
	t.Helper()
	p, err := r.Parse(&input.Data{Bytes: []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// What get prints is accepted as input: the read-only properties are dropped.
// The check is on what reaches the server.
func TestStrip(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusCreated, agentJSON })

	p := parse(t, r, `
id: agent-1
displayName: alpha
meta:
  resourceType: Agent
createdBy:
  value: user-1
oauthClients:
  - clientId: c1
tags: [x]
`)
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "a-1" || obj.Name != "alpha" {
		t.Errorf("object: %+v", obj)
	}
	body := (*calls)[0].Body
	for _, k := range []string{"id", "meta", "createdBy", "oauthClients"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}
	if body["displayName"] != "alpha" || body["tags"] == nil {
		t.Errorf("the writable properties must be kept: %v", body)
	}

	only := parse(t, r, "id: agent-1\nmeta:\n  resourceType: Agent\ncreatedBy:\n  value: u\noauthClients:\n  - clientId: c\n")
	if !only.Empty() {
		t.Error("read-only properties only should be empty")
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusOK, agentJSON })
	conn.Token = ""
	p := parse(t, r, "displayName: alpha\n")

	checks := map[string]error{
		"get":  func() error { _, err := r.Get(t.Context(), conn, "a-1"); return err }(),
		"list": func() error { _, err := r.List(t.Context(), conn, resource.ListQuery{}); return err }(),
		"find": func() error { _, err := r.Find(t.Context(), conn, "alpha"); return err }(),
		"create": func() error {
			_, err := r.Create(t.Context(), conn, p)
			return err
		}(),
		"replace": func() error { _, err := r.Replace(t.Context(), conn, "a-1", p); return err }(),
		"patch":   func() error { _, err := r.Patch(t.Context(), conn, "a-1", p); return err }(),
		"delete":  r.Delete(t.Context(), conn, "a-1"),
	}
	for verb, err := range checks {
		if !errors.Is(err, resource.ErrUnauthorized) {
			t.Errorf("%s without a token: %v", verb, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("no request may be sent without a token: %v", *calls)
	}
}

func TestGet(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusOK, agentJSON })

	obj, err := r.Get(t.Context(), conn, "a-1")
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "a-1" || obj.Name != "alpha" {
		t.Errorf("object: %+v", obj)
	}
	if got := obj.Value.(*Agent); got.Status == nil || *got.Status != "ACTIVE" {
		t.Errorf("status: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/Agents/a-1" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"schemas":["urn:ietf:params:scim:api:messages:2.0:ListResponse"],"totalResults":3,"Resources":[
		{"schemas":["s"],"id":"a-1","displayName":"alpha","status":"ACTIVE","tags":["x","y"],"description":"first"},
		{"schemas":["s"],"id":"a-2","displayName":"beta"},
		{"schemas":["s"],"id":"a-3","displayName":"gamma"}]}`
	conn, calls := mock(t, func(call) (int, string) { return http.StatusOK, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 3 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*Agent)[0]
	if first.Id == nil || *first.Id != "a-1" || first.DisplayName != "alpha" || first.Status == nil || *first.Status != "ACTIVE" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "STATUS", "TAGS", "DESCRIPTION", "a-1", "alpha", "ACTIVE", "x,y", "first"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/Agents" || len(c.Query["filter"]) != 0 || len(c.Query["sortBy"]) != 0 {
		t.Errorf("request: %+v", c)
	}

	// Search, sort and paging.
	if _, err := r.List(t.Context(), conn, resource.ListQuery{Search: "alpha", Sort: "-name", Limit: 1, Page: 2}); err != nil {
		t.Fatal(err)
	}
	q := (*calls)[1].Query
	if q.Get("filter") != `displayName eq "alpha"` || q.Get("sortBy") != "displayName" || q.Get("sortOrder") != "descending" {
		t.Errorf("query: %v", q)
	}

	page, err := r.List(t.Context(), conn, resource.ListQuery{Limit: 1, Page: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Len != 1 || *page.Items.([]*Agent)[0].Id != "a-2" {
		t.Errorf("page 2 of 1: %+v", page.Items)
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, string) { return http.StatusOK, `{"totalResults":0}` })
	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 0 || l.Items == nil {
		t.Errorf("an empty list is an empty, non-nil slice: %+v", l)
	}
}

func TestFind(t *testing.T) {
	r := Resource()
	one := `{"Resources":[{"id":"a-1","displayName":"alpha","schemas":["s"]},{"id":"a-9","displayName":"alphabet","schemas":["s"]}]}`
	conn, calls := mock(t, func(call) (int, string) { return http.StatusOK, one })

	id, err := r.Find(t.Context(), conn, "alpha")
	if err != nil || id != "a-1" {
		t.Errorf("find: %q %v", id, err)
	}
	if got := (*calls)[0].Query.Get("filter"); got != `displayName eq "alpha"` {
		t.Errorf("filter: %q", got)
	}
	if _, err := r.Find(t.Context(), conn, "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("missing name: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusCreated, agentJSON })

	p := parse(t, r, "displayName: alpha\ndescription: d\nstatus: ACTIVE\ntags: [x]\nexternalIds: [e1]\nowners:\n  - type: User\n    value: u1\n")
	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "a-1" {
		t.Errorf("object: %+v", obj)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/Agents" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
	if c.Body["displayName"] != "alpha" || c.Body["description"] != "d" || c.Body["status"] != "ACTIVE" {
		t.Errorf("body: %v", c.Body)
	}
	schemas, _ := c.Body["schemas"].([]any)
	if len(schemas) != 1 || schemas[0] != "urn:ietf:params:scim:schemas:core:ibm:2.0:Agent" {
		t.Errorf("the SCIM schema must be added: %v", c.Body["schemas"])
	}
	for _, k := range []string{"tags", "externalIds", "owners"} {
		if c.Body[k] == nil {
			t.Errorf("%s is missing from the body: %v", k, c.Body)
		}
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusOK, agentJSON })

	obj, err := r.Replace(t.Context(), conn, "a-1", parse(t, r, "id: a-1\ndisplayName: alpha\ntags: [x]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "a-1" {
		t.Errorf("object: %+v", obj)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/Agents/a-1" {
		t.Errorf("request: %+v", c)
	}
	if c.Body["displayName"] != "alpha" || c.Body["id"] != nil || c.Body["schemas"] == nil {
		t.Errorf("body: %v", c.Body)
	}

	// A payload that names another agent is refused before any request.
	n := len(*calls)
	if _, err := r.Replace(t.Context(), conn, "a-2", parse(t, r, "id: a-1\ndisplayName: alpha\n")); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("id mismatch: %v", err)
	}
	if len(*calls) != n {
		t.Error("no request may be sent on an id mismatch")
	}
}

func TestPatch(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, string) {
		if c.Method == http.MethodPatch {
			return http.StatusNoContent, ""
		}
		return http.StatusOK, agentJSON
	})

	obj, err := r.Patch(t.Context(), conn, "a-1", parse(t, r, "displayName: alpha\ndescription: new\nstatus: SUSPENDED\ntags: [x]\nexternalIds: [e]\nowners:\n  - type: User\n    value: u1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "a-1" {
		t.Errorf("object: %+v", obj)
	}
	if len(*calls) != 2 || (*calls)[0].Method != http.MethodPatch || (*calls)[1].Method != http.MethodGet {
		t.Fatalf("a patch is followed by a get of the result: %+v", *calls)
	}
	c := (*calls)[0]
	if c.Path != "/v1.0/Agents/a-1" {
		t.Errorf("path: %s", c.Path)
	}
	schemas, _ := c.Body["schemas"].([]any)
	if len(schemas) != 1 || schemas[0] != "urn:ietf:params:scim:api:messages:2.0:PatchOp" {
		t.Errorf("schemas: %v", c.Body["schemas"])
	}
	ops, _ := c.Body["Operations"].([]any)
	if len(ops) != 6 {
		t.Fatalf("operations: %v", ops)
	}
	got := map[string]any{}
	for _, o := range ops {
		m := o.(map[string]any)
		if m["op"] != "replace" {
			t.Errorf("op: %v", m)
		}
		got[m["path"].(string)] = m["value"]
	}
	if got["displayName"] != "alpha" || got["description"] != "new" || got["status"] != "SUSPENDED" {
		t.Errorf("values: %v", got)
	}
	for _, k := range []string{"tags", "externalIds", "owners"} {
		if _, ok := got[k].([]any); !ok {
			t.Errorf("%s must be a list: %v", k, got[k])
		}
	}
}

func TestPatchClearsDescription(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, string) {
		if c.Method == http.MethodPatch {
			return http.StatusNoContent, ""
		}
		return http.StatusOK, agentJSON
	})
	if _, err := r.Patch(t.Context(), conn, "a-1", parse(t, r, "description: ''\n")); err != nil {
		t.Fatal(err)
	}
	ops, _ := (*calls)[0].Body["Operations"].([]any)
	if len(ops) != 1 {
		t.Fatalf("operations: %v", ops)
	}
	if m := ops[0].(map[string]any); m["op"] != "remove" || m["path"] != "description" {
		t.Errorf("operation: %v", m)
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, string) { return http.StatusNoContent, "" })

	if err := r.Delete(t.Context(), conn, "a-1"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.Method != http.MethodDelete || c.Path != "/v1.0/Agents/a-1" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

// Every verb turns the server's error status into the CLI's errors: 401 and
// 404 become the sentinels, and the others carry the server's detail.
func TestErrorTranslation(t *testing.T) {
	r := Resource()

	verbs := map[string]func(conn resource.Conn) error{
		"get": func(c resource.Conn) error { _, err := r.Get(t.Context(), c, "a-1"); return err },
		"list": func(c resource.Conn) error {
			_, err := r.List(t.Context(), c, resource.ListQuery{})
			return err
		},
		"create": func(c resource.Conn) error {
			_, err := r.Create(t.Context(), c, parse(t, r, "displayName: alpha\n"))
			return err
		},
		"replace": func(c resource.Conn) error {
			_, err := r.Replace(t.Context(), c, "a-1", parse(t, r, "displayName: alpha\n"))
			return err
		},
		"patch": func(c resource.Conn) error {
			_, err := r.Patch(t.Context(), c, "a-1", parse(t, r, "displayName: alpha\n"))
			return err
		},
		"delete": func(c resource.Conn) error { return r.Delete(t.Context(), c, "a-1") },
	}

	cases := []struct {
		status int
		detail string
		check  func(error) bool
	}{
		{http.StatusUnauthorized, "bad token", func(err error) bool { return errors.Is(err, resource.ErrUnauthorized) }},
		{http.StatusNotFound, "no such agent", func(err error) bool { return errors.Is(err, resource.ErrNotFound) }},
		{http.StatusConflict, "AGT0001 displayName already exists", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "AGT0001 displayName already exists")
		}},
		{http.StatusBadRequest, "AGT0002 invalid displayName", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "AGT0002 invalid displayName")
		}},
	}

	for verb, do := range verbs {
		for _, c := range cases {
			// list has no 404 or 409, and get no 409, in the API.
			if (verb == "list" && (c.status == http.StatusNotFound || c.status == http.StatusConflict)) ||
				(verb == "get" && c.status == http.StatusConflict) ||
				(verb == "delete" && c.status == http.StatusBadRequest) {
				continue
			}
			conn, _ := mock(t, func(call) (int, string) {
				return c.status, scimError(http.StatusText(c.status), c.detail)
			})
			if err := do(conn); !c.check(err) {
				t.Errorf("%s with status %d: %v", verb, c.status, err)
			}
		}
	}
}

// A failure without a SCIM body still says what the server answered.
func TestErrorWithoutScimBody(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, string) { return http.StatusTeapot, "short and stout" })
	_, err := r.Get(t.Context(), conn, "a-1")
	if err == nil || !strings.Contains(err.Error(), "418") || !strings.Contains(err.Error(), "short and stout") {
		t.Errorf("error: %v", err)
	}
}
