package oauthproviders

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

	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
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
		reflect.TypeFor[oauth2.OAuthProviderResponse](),
		reflect.TypeFor[oauth2.OAuthProviderWriteRequest](),
		reflect.TypeFor[oauth2.OAuthProviderListResponse](),
		reflect.TypeFor[oauth2.OAuthProviderTokenSettings](),
		reflect.TypeFor[oauth2.OAuthProviderDCRSettings](),
		reflect.TypeFor[oauth2.OAuthProviderDCREndpointAuthentication](),
		reflect.TypeFor[oauth2.OAuthProviderDCRRegistrationAccessToken](),
		reflect.TypeFor[oauth2.OAuthProviderDCRSoftwareStatementValidation](),
		reflect.TypeFor[oauth2.Error](),
		reflect.TypeFor[oauth2.ListOAuthProvidersParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*oauth2.OAuthProviderResponse
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[oauth2.OAuthProviderResponse](&input.Data{Bytes: b})
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
	p := got[0]
	if p.DefinitionName == "" || p.Issuer == "" || p.TokenSettings == nil || p.JwksSigKeystore == nil {
		t.Errorf("the template should show every kind of property: %+v", p)
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
	if r.Get == nil || r.List == nil || r.Find == nil || r.Replace == nil {
		t.Errorf("oauth-providers missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("oauth-providers must not offer patch verb since the API has no PATCH endpoint")
	}
	if r.Create != nil || r.Delete != nil {
		t.Error("oauth-providers must not offer create or delete: there is one provider for now")
	}
	if r.Name != "oauth-providers" || r.Singular != "oauth-provider" || !reflect.DeepEqual(r.Aliases, []string{"oauth-provider", "oauth_providers", "oauth_provider"}) {
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

const providerJSON = `{"definition_name":"default","issuer":"https://auth.example.com","jwks_sig_keystore":"default-keystore","token_settings":{"signing_algorithm":"RS256","signing_key_store":"default-keystore","signing_key_label":"default-key","access_token_lifetime_seconds":3600,"grant_types":["authorization_code","refresh_token"]},"version":1,"created_at":1000,"updated_at":2000}`

func parse(t *testing.T, r *resource.Resource, text string) *resource.Payload {
	t.Helper()
	p, err := r.Parse(&input.Data{Bytes: []byte(text)})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStrip(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusOK, nil, providerJSON
	})

	p := parse(t, r, `definition_name: default
issuer: https://auth.example.com
version: 2
created_at: 1000
updated_at: 2000
`)
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	if _, err := r.Replace(t.Context(), conn, "default", p); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].Body
	for _, k := range []string{"version", "created_at", "updated_at"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}
	if body["definition_name"] != "default" || body["issuer"] != "https://auth.example.com" {
		t.Errorf("body: %v", body)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, providerJSON })
	conn.Token = ""

	for name, act := range map[string]func() error{
		"get": func() error { _, err := r.Get(t.Context(), conn, "default"); return err },
		"list": func() error {
			_, err := r.List(t.Context(), conn, resource.ListQuery{})
			return err
		},
		"find": func() error { _, err := r.Find(t.Context(), conn, "default"); return err },
		"replace": func() error {
			_, err := r.Replace(t.Context(), conn, "default", parse(t, r, "definition_name: default\nissuer: https://auth.example.com\n"))
			return err
		},
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
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, providerJSON })

	obj, err := r.Get(t.Context(), conn, "default")
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != "default" || obj.Name != "default" {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*oauth2.OAuthProviderResponse)
	if got.Issuer != "https://auth.example.com" {
		t.Errorf("issuer: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/oauth_providers/default" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":2,"count":2,"page":1,"oauth_providers":[
		{"definition_name":"default","issuer":"https://auth.example.com","token_settings":{"signing_algorithm":"RS256","signing_key_label":"def-key"}},
		{"definition_name":"custom","issuer":"https://custom.example.com"}
	]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*oauth2.OAuthProviderResponse)[0]
	if first.DefinitionName != "default" || first.Issuer != "https://auth.example.com" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "ISSUER", "SIGNING_KEY", "ALGORITHM", "default", "https://auth.example.com", "def-key", "RS256"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/oauth_providers" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{"total":3,"count":2,"page":1,"oauth_providers":[{"definition_name":"default","issuer":"https://auth.example.com"},{"definition_name":"other","issuer":"https://other.example.com"}]}`
	page2 := `{"total":3,"count":1,"page":2,"oauth_providers":[{"definition_name":"custom","issuer":"https://custom.example.com"}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "custom"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	res := l.Items.([]*oauth2.OAuthProviderResponse)
	if res[0].DefinitionName != "custom" {
		t.Errorf("match is not custom: %+v", res[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"oauth_providers":[]}`
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
	conn, _ := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":1,"count":1,"page":1,"oauth_providers":[{"definition_name":"default","issuer":"https://auth.example.com"}]}`
	})

	id, err := r.Find(t.Context(), conn, "default")
	if err != nil || id != "default" {
		t.Errorf("find existing: id=%q err=%v", id, err)
	}

	_, err = r.Find(t.Context(), conn, "missing")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, `{"definition_name":"default","issuer":"https://replaced.example.com"}`
		}
		return http.StatusOK, nil, `{"definition_name":"default","issuer":"https://replaced.example.com"}`
	})

	v := parse(t, r, `definition_name: default
issuer: https://replaced.example.com
`)
	replaced, err := r.Replace(t.Context(), conn, "default", v)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != "default" {
		t.Errorf("replaced id: %+v", replaced)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/oauth_providers/default" {
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
		{http.StatusNotFound, `{"error":"not_found","error_description":"No such provider"}`, resource.ErrNotFound, ""},
		{http.StatusBadRequest, `{"error":"invalid_request","error_description":"Bad field"}`, nil, "invalid_request: Bad field"},
		{http.StatusInternalServerError, `{"error":"server_error"}`, nil, "server_error"},
	} {
		conn, _ := mock(t, func(call) (int, http.Header, string) {
			return tc.Status, nil, tc.Body
		})
		_, err := r.Get(t.Context(), conn, "default")
		if tc.Target != nil && !errors.Is(err, tc.Target) {
			t.Errorf("%d: want %v, got %v", tc.Status, tc.Target, err)
		}
		if tc.Text != "" && (err == nil || !strings.Contains(err.Error(), tc.Text)) {
			t.Errorf("%d: error %v should contain %q", tc.Status, err, tc.Text)
		}
	}
}
