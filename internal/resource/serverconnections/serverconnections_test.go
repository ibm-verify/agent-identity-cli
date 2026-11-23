package serverconnections

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

	"github.com/ibm-verify/agent-identity-cli/internal/client/platform"
	"github.com/ibm-verify/agent-identity-cli/internal/input"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

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
		reflect.TypeFor[platform.ServerConnection](),
		reflect.TypeFor[platform.ServerConnectionWriteRequest](),
		reflect.TypeFor[platform.ServerConnectionSummary](),
		reflect.TypeFor[platform.ServerConnectionListResponse](),
		reflect.TypeFor[platform.HTTPConnectionConfig](),
		reflect.TypeFor[platform.LDAPConnectionConfig](),
		reflect.TypeFor[platform.ServerConnectionAuthentication](),
		reflect.TypeFor[platform.HTTPOperation](),
		reflect.TypeFor[platform.HTTPSearch](),
		reflect.TypeFor[platform.HTTPSearchQuery](),
		reflect.TypeFor[platform.LDAPSearchTemplate](),
		reflect.TypeFor[platform.ServerConnectionRetry](),
		reflect.TypeFor[platform.ServerConnectionTLS](),
		reflect.TypeFor[platform.ConnectionTransform](),
		reflect.TypeFor[platform.Error](),
		reflect.TypeFor[platform.ListServerConnectionsParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*platform.ServerConnection
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[platform.ServerConnection](&input.Data{Bytes: b})
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
	if c.Name == "" || c.Type == "" || c.Http == nil || c.Http.BaseUrl == "" ||
		c.Http.TimeoutSeconds == nil || c.Http.Authentication == nil ||
		c.Http.Authentication.Type == "" || c.Http.Authentication.TokenUrl == nil ||
		c.Http.Authentication.ClientId == nil || c.Http.Authentication.ClientSecret == nil ||
		c.Http.Operations == nil {
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
		t.Errorf("serverconnections missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("serverconnections must not offer patch verb since the API has no PATCH endpoint")
	}
	if r.Name != "server-connections" || r.Singular != "server-connection" || !reflect.DeepEqual(r.Aliases, []string{"server-connection", "server_connections", "server_connection"}) {
		t.Errorf("names: %q %q %v", r.Name, r.Singular, r.Aliases)
	}
	if !r.ListFlags.Limit || !r.ListFlags.Page || !r.ListFlags.Search || r.ListFlags.Sort {
		t.Errorf("list flags: %+v", r.ListFlags)
	}
}

type call struct {
	Method string
	Path   string
	Query  url.Values
	Auth   string
	Body   map[string]any
}

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
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
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

func parse(t *testing.T, r *resource.Resource, text string) *resource.Payload {
	t.Helper()
	p, err := r.Parse(&input.Data{Bytes: []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

const connectionJSON = `{
	"name": "my-conn",
	"type": "http",
	"http": {
		"base_url": "https://api.example.com",
		"timeout_seconds": 30,
		"authentication": {
			"type": "oauth",
			"token_url": "https://auth.example.com/oauth2/token",
			"client_id": "my-client-id",
			"client_secret": "my-client-secret",
			"password": "my-password"
		}
	},
	"created_at": "2026-11-23T06:25:39Z",
	"updated_at": "2026-11-23T06:25:39Z"
}`

func TestStripAndMask(t *testing.T) {
	r := Resource()

	// 1. Test Strip: Timestamps stripped, and secrets holding mask are cleared when sent to server
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, http.Header{"Location": []string{"https://server/v1.0/server_connections/my-conn"}}, ""
		}
		return http.StatusOK, nil, connectionJSON
	})

	p := parse(t, r, `
name: my-conn
type: http
created_at: 2026-11-23T06:25:39Z
updated_at: 2026-11-23T06:25:39Z
http:
  base_url: https://api.example.com
  authentication:
    type: oauth
    client_secret: "********"
    password: "********"
`)
	if p.Empty() {
		t.Fatal("payload should not be empty")
	}

	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "my-conn" {
		t.Errorf("object ID mismatch: %s", obj.ID)
	}

	body := (*calls)[0].Body
	for _, k := range []string{"created_at", "updated_at"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}

	httpVal, ok := body["http"].(map[string]any)
	if !ok {
		t.Fatalf("missing http config block in request: %v", body)
	}
	authVal, ok := httpVal["authentication"].(map[string]any)
	if !ok {
		t.Fatalf("missing authentication block in request: %v", httpVal)
	}

	for _, k := range []string{"client_secret", "password"} {
		if _, ok := authVal[k]; ok {
			t.Errorf("%s must be stripped from request when holding mask: %v", k, authVal)
		}
	}

	// 2. Also verify standard non-masked values are kept
	connKept, callsKept := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, http.Header{"Location": []string{"https://server/v1.0/server_connections/my-conn"}}, ""
		}
		return http.StatusOK, nil, connectionJSON
	})

	pKept := parse(t, r, `
name: my-conn
type: http
http:
  base_url: https://api.example.com
  authentication:
    type: oauth
    client_secret: "my-real-secret"
`)
	_, err = r.Create(t.Context(), connKept, pKept)
	if err != nil {
		t.Fatal(err)
	}

	bodyKept := (*callsKept)[0].Body
	httpKept, _ := bodyKept["http"].(map[string]any)
	authKept, _ := httpKept["authentication"].(map[string]any)
	if authKept["client_secret"] != "my-real-secret" {
		t.Errorf("real client_secret should be kept: %v", authKept)
	}

	// 3. Test Mask: Get returns the secrets masked (replaced with resource.Mask)
	connMask, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, connectionJSON
	})

	objMask, err := r.Get(t.Context(), connMask, "my-conn")
	if err != nil {
		t.Fatal(err)
	}
	got := objMask.Value.(*platform.ServerConnection)
	if got.Http.Authentication.ClientSecret == nil || *got.Http.Authentication.ClientSecret != resource.Mask {
		t.Errorf("client_secret was not masked: %v", got.Http.Authentication.ClientSecret)
	}
	if got.Http.Authentication.Password == nil || *got.Http.Authentication.Password != resource.Mask {
		t.Errorf("password was not masked: %v", got.Http.Authentication.Password)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, connectionJSON })
	conn.Token = ""
	p := parse(t, r, "name: my-conn\ntype: http\n")

	checks := map[string]error{
		"get":     func() error { _, err := r.Get(t.Context(), conn, "my-conn"); return err }(),
		"list":    func() error { _, err := r.List(t.Context(), conn, resource.ListQuery{}); return err }(),
		"find":    func() error { _, err := r.Find(t.Context(), conn, "my-conn"); return err }(),
		"create":  func() error { _, err := r.Create(t.Context(), conn, p); return err }(),
		"replace": func() error { _, err := r.Replace(t.Context(), conn, "my-conn", p); return err }(),
		"delete":  r.Delete(t.Context(), conn, "my-conn"),
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
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, connectionJSON })

	obj, err := r.Get(t.Context(), conn, "my-conn")
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "my-conn" || obj.Name != "my-conn" {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*platform.ServerConnection)
	if got.Type != "http" || got.Http.BaseUrl != "https://api.example.com" {
		t.Errorf("fields: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/server_connections/my-conn" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{
		"total": 3,
		"count": 3,
		"page": 1,
		"server_connections": [
			{"name": "conn-1", "type": "http"},
			{"name": "conn-2", "type": "ldap"},
			{"name": "conn-3", "type": "http"}
		]
	}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 3 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*platform.ServerConnection)[0]
	if first.Name != "conn-1" || first.Type != "http" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "TYPE", "ENDPOINT", "conn-1", "http", "conn-2", "ldap"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/server_connections" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{
		"total": 3,
		"count": 2,
		"page": 1,
		"server_connections": [
			{"name": "conn-1", "type": "http"},
			{"name": "conn-2", "type": "ldap"}
		]
	}`
	page2 := `{
		"total": 3,
		"count": 1,
		"page": 2,
		"server_connections": [
			{"name": "conn-3", "type": "http"}
		]
	}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "conn-3"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	res := l.Items.([]*platform.ServerConnection)
	if res[0].Name != "conn-3" {
		t.Errorf("match is not conn-3: %+v", res[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"server_connections":[]}`
	})
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
	one := `{
		"total": 2,
		"count": 2,
		"server_connections": [
			{"name": "conn-1", "type": "http"},
			{"name": "conn-2", "type": "ldap"}
		]
	}`
	conn, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, one })

	id, err := r.Find(t.Context(), conn, "conn-1")
	if err != nil || id != "conn-1" {
		t.Errorf("find: %q %v", id, err)
	}
	if _, err := r.Find(t.Context(), conn, "nope"); !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("missing name: %v", err)
	}

	dup := `{
		"total": 2,
		"count": 2,
		"server_connections": [
			{"name": "conn-1", "type": "http"},
			{"name": "conn-1", "type": "ldap"}
		]
	}`
	connDup, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, dup })
	if _, err := r.Find(t.Context(), connDup, "conn-1"); !errors.Is(err, resource.ErrAmbiguousName) {
		t.Errorf("ambiguous name: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := Resource()

	// Case 1: Post returns 201 with Location header, no response body.
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, http.Header{"Location": []string{"https://server/v1.0/server_connections/my-conn"}}, ""
		}
		return http.StatusOK, nil, connectionJSON
	})

	p := parse(t, r, "name: my-conn\ntype: http\nhttp:\n  base_url: https://api.example.com\n")
	obj, err := r.Create(t.Context(), conn, p)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "my-conn" {
		t.Errorf("object: %+v", obj)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected POST followed by GET, got %d calls", len(*calls))
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/server_connections" {
		t.Errorf("request: %+v", c)
	}

	// Case 2: Post returns 201 with a response body directly.
	conn2, calls2 := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusCreated, nil, connectionJSON
	})
	obj2, err2 := r.Create(t.Context(), conn2, p)
	if err2 != nil {
		t.Fatal(err2)
	}
	if obj2.ID != "my-conn" {
		t.Errorf("object: %+v", obj2)
	}
	if len(*calls2) != 1 {
		t.Fatalf("expected POST only when response contains 201 JSON body, got %d calls", len(*calls2))
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, ""
		}
		return http.StatusOK, nil, connectionJSON
	})

	obj, err := r.Replace(t.Context(), conn, "my-conn", parse(t, r, "name: my-conn\ntype: http\n"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "my-conn" {
		t.Errorf("object: %+v", obj)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected PUT followed by GET, got %d calls", len(*calls))
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/server_connections/my-conn" {
		t.Errorf("request: %+v", c)
	}

	// A payload that names another serverconnection is refused before any request.
	n := len(*calls)
	if _, err := r.Replace(t.Context(), conn, "other-conn", parse(t, r, "name: my-conn\ntype: http\n")); !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("id mismatch: %v", err)
	}
	if len(*calls) != n {
		t.Error("no request may be sent on an id mismatch")
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusNoContent, nil, "" })

	if err := r.Delete(t.Context(), conn, "my-conn"); err != nil {
		t.Fatal(err)
	}
	if c := (*calls)[0]; c.Method != http.MethodDelete || c.Path != "/v1.0/server_connections/my-conn" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestErrorTranslation(t *testing.T) {
	r := Resource()

	verbs := map[string]func(conn resource.Conn) error{
		"get": func(c resource.Conn) error { _, err := r.Get(t.Context(), c, "my-conn"); return err },
		"list": func(c resource.Conn) error {
			_, err := r.List(t.Context(), c, resource.ListQuery{})
			return err
		},
		"create": func(c resource.Conn) error {
			_, err := r.Create(t.Context(), c, parse(t, r, "name: my-conn\ntype: http\n"))
			return err
		},
		"replace": func(c resource.Conn) error {
			_, err := r.Replace(t.Context(), c, "my-conn", parse(t, r, "name: my-conn\ntype: http\n"))
			return err
		},
		"delete": func(c resource.Conn) error { return r.Delete(t.Context(), c, "my-conn") },
	}

	cases := []struct {
		status  int
		errCode string
		desc    string
		check   func(error) bool
	}{
		{http.StatusUnauthorized, "invalid_token", "bad token", func(err error) bool { return errors.Is(err, resource.ErrUnauthorized) }},
		{http.StatusNotFound, "not_found", "no such connection", func(err error) bool { return errors.Is(err, resource.ErrNotFound) }},
		{http.StatusConflict, "already_exists", "name already exists", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "already_exists") && strings.Contains(err.Error(), "name already exists")
		}},
		{http.StatusBadRequest, "invalid_payload", "invalid properties", func(err error) bool {
			return err != nil && strings.Contains(err.Error(), "invalid_payload") && strings.Contains(err.Error(), "invalid properties")
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
				return c.status, nil, `{"error":"` + c.errCode + `","error_description":"` + c.desc + `"}`
			})
			if err := do(conn); !c.check(err) {
				t.Errorf("%s with status %d: %v", verb, c.status, err)
			}
		}
	}
}

func TestErrorWithoutBody(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusBadRequest, nil, ""
	})
	_, err := r.Get(t.Context(), conn, "my-conn")
	if err == nil || !strings.Contains(err.Error(), "server returned status code 400") {
		t.Errorf("expected generic error with status code: %v", err)
	}
}
