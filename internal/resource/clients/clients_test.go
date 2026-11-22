package clients

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
		reflect.TypeFor[Client](),
		reflect.TypeFor[ClientListResponse](),
		reflect.TypeFor[ClientMetadata](),
		reflect.TypeFor[ClientPayload](),
		reflect.TypeFor[ClientSummary](),
		reflect.TypeFor[ClientWriteRequest](),
		reflect.TypeFor[DynamicClientExtension](),
		reflect.TypeFor[DynamicClientRequest](),
		reflect.TypeFor[Error](),
		reflect.TypeFor[OnPremClientFields](),
		reflect.TypeFor[SingleLogoutSettings](),
		reflect.TypeFor[TokenExchangeSettings](),
		reflect.TypeFor[AuthorizationDetailsTypesFilter](),
	} {
		check(typ)
	}
}

// The template is what users start from, so it must decode in both formats
// and give the same object.
func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*Client
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[Client](&input.Data{Bytes: b})
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
	c := got[0]
	if c.ClientName == nil || *c.ClientName == "" || c.Enabled == nil || c.GrantTypes == nil || len(*c.GrantTypes) == 0 ||
		c.ResponseTypes == nil || len(*c.ResponseTypes) == 0 || len(c.RedirectUris) == 0 ||
		c.TokenEndpointAuthMethod == nil || c.Scope == nil || c.Tags == nil || len(*c.Tags) == 0 {
		t.Errorf("the template should show every kind of property: %+v", c)
	}
	b, err := r.Template("yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# ") {
		t.Error("the yaml template should explain each property")
	}
}

func TestResourceOffersConfiguredVerbs(t *testing.T) {
	r := Resource()
	if r.Get == nil || r.List == nil || r.Find == nil || r.Create == nil || r.Replace == nil || r.Delete == nil {
		t.Errorf("clients missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("clients must not offer patch verb since the API has no PATCH endpoint")
	}
	if r.Name != "clients" || r.Singular != "client" || !reflect.DeepEqual(r.Aliases, []string{"client"}) {
		t.Errorf("names: %q %q %v", r.Name, r.Singular, r.Aliases)
	}
	if !r.ListFlags.Limit || !r.ListFlags.Page || !r.ListFlags.Search || r.ListFlags.Sort {
		t.Errorf("list flags: %+v", r.ListFlags)
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

// mock starts a server that records requests and answers each with h's status,
// headers, and body.
func mock(t *testing.T, h func(c call) (int, http.Header, string)) (resource.Conn, *[]call) {
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
		status, headers, body := h(c)
		w.Header().Set("Content-Type", "application/json")
		for k, v := range headers {
			for _, val := range v {
				w.Header().Add(k, val)
			}
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return resource.Conn{Server: srv.URL, Token: "tok", HTTP: srv.Client()}, &calls
}

func oidcError(errCode, desc string) string {
	return `{"error":"` + errCode + `","error_description":"` + desc + `"}`
}

const clientJSON = `{"client_id":"c-1","client_name":"alpha","enabled":true,"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"redirect_uris":["https://app.example.com/callback"],"token_endpoint_auth_method":"client_secret_basic","scope":"openid profile","tags":["x","y"]}`

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
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, http.Header{"Location": []string{"/v1.0/oauth_clients/c-1"}}, ""
		}
		return http.StatusOK, nil, clientJSON
	})

	p := parse(t, r, `
client_id: c-1
client_name: alpha
client_id_issued_at: 1700000000
client_secret_expires_at: 0
definition_name: default
created_at: 1700000000000
tags: [x]
`)
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "c-1" || obj.Name != "alpha" {
		t.Errorf("object: %+v", obj)
	}
	body := (*calls)[0].Body
	for _, k := range []string{"client_id_issued_at", "client_secret_expires_at", "definition_name", "created_at"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}
	if body["client_name"] != "alpha" || body["tags"] == nil {
		t.Errorf("the writable properties must be kept: %v", body)
	}

	only := parse(t, r, "client_id_issued_at: 1700000000\nclient_secret_expires_at: 0\ndefinition_name: default\ncreated_at: 1700000000000\n")
	if !only.Empty() {
		t.Error("read-only properties only should be empty")
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, clientJSON })
	conn.Token = ""
	p := parse(t, r, "client_name: alpha\n")

	checks := map[string]error{
		"get":     func() error { _, err := r.Get(t.Context(), conn, "c-1"); return err }(),
		"list":    func() error { _, err := r.List(t.Context(), conn, resource.ListQuery{}); return err }(),
		"find":    func() error { _, err := r.Find(t.Context(), conn, "alpha"); return err }(),
		"create":  func() error { _, err := r.Create(t.Context(), conn, p); return err }(),
		"replace": func() error { _, err := r.Replace(t.Context(), conn, "c-1", p); return err }(),
		"delete":  r.Delete(t.Context(), conn, "c-1"),
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
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, clientJSON })

	obj, err := r.Get(t.Context(), conn, "c-1")
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "c-1" || obj.Name != "alpha" {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*Client)
	if got.Enabled == nil || !*got.Enabled {
		t.Errorf("enabled: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/oauth_clients/c-1" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":3,"count":3,"page":1,"clients":[
		{"client_id":"c-1","client_name":"alpha","enabled":true,"grant_types":["authorization_code","refresh_token"],"tags":["x","y"]},
		{"client_id":"c-2","client_name":"beta"},
		{"client_id":"c-3","client_name":"gamma"}]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 3 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*Client)[0]
	if first.ClientId == nil || *first.ClientId != "c-1" || first.ClientName == nil || *first.ClientName != "alpha" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "CLIENT_NAME", "GRANT_TYPES", "ENABLED", "TAGS", "c-1", "alpha", "x,y"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/oauth_clients" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
		t.Errorf("request: %+v", c)
	}

	// Paging with limit and page.
	if _, err := r.List(t.Context(), conn, resource.ListQuery{Limit: 10, Page: 2}); err != nil {
		t.Fatal(err)
	}
	q := (*calls)[1].Query
	if q.Get("limit") != "10" || q.Get("page") != "2" {
		t.Errorf("query: %v", q)
	}
}

func TestListSearch(t *testing.T) {
	r := Resource()
	page1 := `{"total":3,"count":2,"page":1,"clients":[{"client_id":"c-1","client_name":"alpha"},{"client_id":"c-2","client_name":"beta"}]}`
	page2 := `{"total":3,"count":1,"page":2,"clients":[{"client_id":"c-3","client_name":"alpha"}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("expected 2 matches, got %d", l.Len)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, `{"total":0,"count":0,"clients":[]}` })
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
	one := `{"total":2,"count":2,"clients":[{"client_id":"c-1","client_name":"alpha"},{"client_id":"c-9","client_name":"alphabet"}]}`
	conn, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, one })

	id, err := r.Find(t.Context(), conn, "alpha")
	if err != nil || id != "c-1" {
		t.Errorf("find: %q %v", id, err)
	}
	if _, err := r.Find(t.Context(), conn, "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("missing name: %v", err)
	}

	dup := `{"total":2,"count":2,"clients":[{"client_id":"c-1","client_name":"alpha"},{"client_id":"c-2","client_name":"alpha"}]}`
	connDup, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, dup })
	if _, err := r.Find(t.Context(), connDup, "alpha"); !errors.Is(err, resource.ErrAmbiguousName) {
		t.Errorf("ambiguous name: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, http.Header{"Location": []string{"https://server/v1.0/oauth_clients/c-1"}}, ""
		}
		return http.StatusOK, nil, clientJSON
	})

	p := parse(t, r, "client_name: alpha\nenabled: true\ngrant_types:\n  - authorization_code\nredirect_uris:\n  - https://app.example.com/callback\ntags: [x]\n")
	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "c-1" {
		t.Errorf("object: %+v", obj)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected POST followed by GET, got %d calls", len(*calls))
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/oauth_clients" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
	if c.Body["client_name"] != "alpha" || c.Body["enabled"] != true {
		t.Errorf("body: %v", c.Body)
	}
	for _, k := range []string{"grant_types", "redirect_uris", "tags"} {
		if c.Body[k] == nil {
			t.Errorf("%s is missing from the body: %v", k, c.Body)
		}
	}
	getCall := (*calls)[1]
	if getCall.Method != http.MethodGet || getCall.Path != "/v1.0/oauth_clients/c-1" {
		t.Errorf("get call: %+v", getCall)
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, ""
		}
		return http.StatusOK, nil, clientJSON
	})

	obj, err := r.Replace(t.Context(), conn, "c-1", parse(t, r, "client_id: c-1\nclient_name: alpha\ntags: [x]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "c-1" {
		t.Errorf("object: %+v", obj)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected PUT followed by GET, got %d calls", len(*calls))
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/oauth_clients/c-1" {
		t.Errorf("request: %+v", c)
	}
	if c.Body["client_name"] != "alpha" {
		t.Errorf("body: %v", c.Body)
	}

	// A payload that names another client is refused before any request.
	n := len(*calls)
	if _, err := r.Replace(t.Context(), conn, "c-2", parse(t, r, "client_id: c-1\nclient_name: alpha\n")); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("id mismatch: %v", err)
	}
	if len(*calls) != n {
		t.Error("no request may be sent on an id mismatch")
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusNoContent, nil, "" })

	if err := r.Delete(t.Context(), conn, "c-1"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.Method != http.MethodDelete || c.Path != "/v1.0/oauth_clients/c-1" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

// Every verb turns the server's error status into the CLI's errors: 401 and
// 404 become the sentinels, and the others carry the server's detail.
func TestErrorTranslation(t *testing.T) {
	r := Resource()

	verbs := map[string]func(conn resource.Conn) error{
		"get": func(c resource.Conn) error { _, err := r.Get(t.Context(), c, "c-1"); return err },
		"list": func(c resource.Conn) error {
			_, err := r.List(t.Context(), c, resource.ListQuery{})
			return err
		},
		"create": func(c resource.Conn) error {
			_, err := r.Create(t.Context(), c, parse(t, r, "client_name: alpha\n"))
			return err
		},
		"replace": func(c resource.Conn) error {
			_, err := r.Replace(t.Context(), c, "c-1", parse(t, r, "client_name: alpha\n"))
			return err
		},
		"delete": func(c resource.Conn) error { return r.Delete(t.Context(), c, "c-1") },
	}

	cases := []struct {
		status  int
		errCode string
		desc    string
		check   func(error) bool
	}{
		{http.StatusUnauthorized, "invalid_token", "bad token", func(err error) bool { return errors.Is(err, resource.ErrUnauthorized) }},
		{http.StatusNotFound, "not_found", "no such client", func(err error) bool { return errors.Is(err, resource.ErrNotFound) }},
		{http.StatusConflict, "invalid_client_metadata", "client_name already exists", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "invalid_client_metadata") && strings.Contains(err.Error(), "client_name already exists")
		}},
		{http.StatusBadRequest, "invalid_client_metadata", "invalid redirect_uris", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "invalid_client_metadata") && strings.Contains(err.Error(), "invalid redirect_uris")
		}},
	}

	for verb, do := range verbs {
		for _, c := range cases {
			// list and delete have no 404 or 409 in the API. get has no 409.
			if (verb == "list" && (c.status == http.StatusNotFound || c.status == http.StatusConflict)) ||
				(verb == "get" && c.status == http.StatusConflict) ||
				(verb == "delete" && (c.status == http.StatusNotFound || c.status == http.StatusConflict || c.status == http.StatusBadRequest)) {
				continue
			}
			conn, _ := mock(t, func(call) (int, http.Header, string) {
				return c.status, nil, oidcError(c.errCode, c.desc)
			})
			if err := do(conn); !c.check(err) {
				t.Errorf("%s with status %d: %v", verb, c.status, err)
			}
		}
	}
}

// A failure without an OIDC Error body still says what the server answered.
func TestErrorWithoutBody(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusTeapot, nil, "short and stout" })
	_, err := r.Get(t.Context(), conn, "c-1")
	if err == nil || !strings.Contains(err.Error(), "418") || !strings.Contains(err.Error(), "short and stout") {
		t.Errorf("error: %v", err)
	}
}
