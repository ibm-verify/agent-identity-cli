package keystores

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
		reflect.TypeFor[Keystore](),
		reflect.TypeFor[platform.KeystoreSummary](),
		reflect.TypeFor[platform.KeystoreWriteRequest](),
		reflect.TypeFor[platform.KeystoreListResponse](),
		reflect.TypeFor[platform.Error](),
		reflect.TypeFor[platform.ListKeystoresParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*Keystore
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[Keystore](&input.Data{Bytes: b})
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
	k := got[0]
	if k.KeystoreName == "" || k.KeystoreType == "" || k.KeystoreData == nil || k.KeystorePassword == nil {
		t.Errorf("the template should show every kind of property: %+v", k)
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
		t.Errorf("keystores missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("keystores must not offer patch verb since the API has no PATCH endpoint")
	}
	if r.Name != "keystores" || r.Singular != "keystore" || !reflect.DeepEqual(r.Aliases, []string{"keystore", "key-stores", "key-store"}) {
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
			_ = json.Unmarshal(b, &c.Body)
		}
		status, headers, body := h(c)
		calls = append(calls, c)
		for k, vs := range headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return resource.Conn{Server: srv.URL, Token: "tok"}, &calls
}

const keystoreJSON = `{"keystore_name":"my-keystore","keystore_type":"p12","created_at":1000,"updated_at":2000}`

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
			return http.StatusCreated, nil, ""
		}
		return http.StatusOK, nil, keystoreJSON
	})

	p := parse(t, r, `keystore_name: my-keystore
keystore_type: p12
keystore_data: ZGF0YQ==
keystore_password: secret
created_at: 1000
updated_at: 2000
`)
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	if _, err := r.Create(t.Context(), conn, p); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].Body
	for _, k := range []string{"created_at", "updated_at"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}
	if body["keystore_name"] != "my-keystore" || body["keystore_type"] != "p12" ||
		body["keystore_data"] != "ZGF0YQ==" || body["keystore_password"] != "secret" {
		t.Errorf("body: %v", body)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, keystoreJSON })
	conn.Token = ""
	payload := "keystore_name: my-keystore\nkeystore_type: p12\n"

	for name, act := range map[string]func() error{
		"get": func() error { _, err := r.Get(t.Context(), conn, "my-keystore"); return err },
		"list": func() error {
			_, err := r.List(t.Context(), conn, resource.ListQuery{})
			return err
		},
		"find": func() error { _, err := r.Find(t.Context(), conn, "my-keystore"); return err },
		"create": func() error {
			_, err := r.Create(t.Context(), conn, parse(t, r, payload))
			return err
		},
		"replace": func() error {
			_, err := r.Replace(t.Context(), conn, "my-keystore", parse(t, r, payload))
			return err
		},
		"delete": func() error { return r.Delete(t.Context(), conn, "my-keystore") },
	} {
		if err := act(); !errors.Is(err, resource.ErrUnauthorized) {
			t.Errorf("%s with no token: %v", name, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("no request may be sent without a token: %v", *calls)
	}
}

func TestGet(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, keystoreJSON })

	obj, err := r.Get(t.Context(), conn, "my-keystore")
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "my-keystore" || obj.Name != "my-keystore" {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*Keystore)
	if got.KeystoreType != "p12" || got.KeystoreData != nil || got.KeystorePassword != nil {
		t.Errorf("keystore: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/keystores/my-keystore" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":2,"count":2,"page":1,"keystores":[
		{"keystore_name":"first","keystore_type":"p12","updated_at":2000},
		{"keystore_name":"second","keystore_type":"pem"}
	]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*Keystore)[0]
	if first.KeystoreName != "first" || first.KeystoreType != "p12" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "TYPE", "UPDATED_AT", "first", "second", "p12", "pem", "2000"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/keystores" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{"total":3,"count":2,"page":1,"keystores":[{"keystore_name":"a","keystore_type":"p12"},{"keystore_name":"b","keystore_type":"p12"}]}`
	page2 := `{"total":3,"count":1,"page":2,"keystores":[{"keystore_name":"wanted","keystore_type":"pem"}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "wanted"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	if got := l.Items.([]*Keystore)[0]; got.KeystoreName != "wanted" {
		t.Errorf("match: %+v", got)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"keystores":[]}`
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
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":1,"count":1,"page":1,"keystores":[{"keystore_name":"my-keystore","keystore_type":"p12"}]}`
	})

	id, err := r.Find(t.Context(), conn, "my-keystore")
	if err != nil || id != "my-keystore" {
		t.Errorf("find existing: id=%q err=%v", id, err)
	}

	_, err = r.Find(t.Context(), conn, "missing")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := Resource()
	h := http.Header{}
	h.Set("Location", "/v1.0/keystores/created-ks")
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, h, ""
		}
		return http.StatusOK, nil, `{"keystore_name":"created-ks","keystore_type":"pem","created_at":1000,"updated_at":1000}`
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, "keystore_name: created-ks\nkeystore_type: pem\nkeystore_data: '{}'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "created-ks" {
		t.Errorf("created id: %+v", created)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected a create and a read back, got %v", *calls)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/keystores" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
	if rb := (*calls)[1]; rb.Method != http.MethodGet || rb.Path != "/v1.0/keystores/created-ks" {
		t.Errorf("read back: %+v", rb)
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, ""
		}
		return http.StatusOK, nil, `{"keystore_name":"my-keystore","keystore_type":"pem","updated_at":3000}`
	})

	replaced, err := r.Replace(t.Context(), conn, "my-keystore", parse(t, r, "keystore_name: my-keystore\nkeystore_type: pem\nkeystore_data: '{}'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != "my-keystore" || replaced.Value.(*Keystore).KeystoreType != "pem" {
		t.Errorf("replaced: %+v", replaced)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/keystores/my-keystore" || c.Body["keystore_type"] != "pem" {
		t.Errorf("request: %+v", c)
	}

	// The name in the data must be the target.
	_, err = r.Replace(t.Context(), conn, "my-keystore", parse(t, r, "keystore_name: other\nkeystore_type: pem\n"))
	if err == nil {
		t.Error("a different keystore_name should be refused")
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusNoContent, nil, "" })

	if err := r.Delete(t.Context(), conn, "my-keystore"); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.Method != http.MethodDelete || c.Path != "/v1.0/keystores/my-keystore" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestErrorTranslation(t *testing.T) {
	r := Resource()
	for _, tc := range []struct {
		Status int
		Body   string
		Target error
		Text   string
	}{
		{http.StatusUnauthorized, "", resource.ErrUnauthorized, ""},
		{http.StatusNotFound, `{"error":"not_found","error_description":"No such keystore"}`, resource.ErrNotFound, ""},
		{http.StatusForbidden, `{"error":"forbidden","error_description":"Insufficient permissions"}`, nil, "forbidden: Insufficient permissions"},
		{http.StatusInternalServerError, `{"error":"server_error"}`, nil, "server_error"},
	} {
		conn, _ := mock(t, func(call) (int, http.Header, string) { return tc.Status, nil, tc.Body })
		_, err := r.Get(t.Context(), conn, "my-keystore")
		if tc.Target != nil && !errors.Is(err, tc.Target) {
			t.Errorf("%d: want %v, got %v", tc.Status, tc.Target, err)
		}
		if tc.Text != "" && (err == nil || !strings.Contains(err.Error(), tc.Text)) {
			t.Errorf("%d: error %v should contain %q", tc.Status, err, tc.Text)
		}
	}
}

func TestCreateConflict(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusConflict, nil, `{"error":"already_exists","error_description":"keystore_name already exists"}`
	})
	_, err := r.Create(t.Context(), conn, parse(t, r, "keystore_name: my-keystore\nkeystore_type: p12\n"))
	if err == nil || !strings.Contains(err.Error(), "already_exists: keystore_name already exists") {
		t.Errorf("conflict: %v", err)
	}
}

func TestErrorWithoutBody(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) { return http.StatusBadGateway, nil, "" })
	_, err := r.Get(t.Context(), conn, "my-keystore")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Errorf("error should carry the status code: %v", err)
	}
}
