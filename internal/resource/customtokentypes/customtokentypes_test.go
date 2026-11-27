package customtokentypes

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

const (
	typeName = "urn:test:com"
	// typeID is the base64url of typeName without padding, as the server derives it.
	typeID  = "dXJuOnRlc3Q6Y29t"
	jwtType = "urn:ietf:params:oauth:token-type:jwt"
)

func TestTagsMatch(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
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
		reflect.TypeFor[oauth2.CustomTokenType](),
		reflect.TypeFor[oauth2.CustomTokenTypeWriteRequest](),
		reflect.TypeFor[oauth2.CustomTokenTypeListResponse](),
		reflect.TypeFor[oauth2.CustomTokenTypeValidate](),
		reflect.TypeFor[oauth2.CustomTokenTypeIdentityMapping](),
		reflect.TypeFor[oauth2.Error](),
		reflect.TypeFor[oauth2.ListCustomTokenTypesParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*oauth2.CustomTokenType
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[oauth2.CustomTokenType](&input.Data{Bytes: b})
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
	v := got[0]
	if v.Name == "" || string(v.Type) != jwtType || v.Issuer == "" || v.Validate.JwksUri == nil || v.Validate.AllowedSigningAlgs == nil {
		t.Errorf("the template should carry every required property: %+v", v)
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
		t.Errorf("custom-token-types missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("custom-token-types must not offer patch since the API has no PATCH endpoint")
	}
	if r.Name != "custom-token-types" || r.Singular != "custom-token-type" || !reflect.DeepEqual(r.Aliases, []string{"custom-token-type", "custom_token_types", "custom_token_type"}) {
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
		// Like the server, send no content type with an empty body.
		if body != "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return resource.Conn{Server: srv.URL, Token: "tok"}, &calls
}

const typeJSON = `{"id":"` + typeID + `","name":"` + typeName + `","type":"` + jwtType + `","issuer":"https://idp.example.com","validate":{"jwksUri":"https://idp.example.com/jwks","allowedSigningAlgs":["RS256"],"allowedKids":[],"validateJti":false,"timeSkew":30,"toleranceWindow":3600},"identityMapping":{"subjectId":"sub","searchBy":"username"},"created_at":1000,"updated_at":2000}`

const minimalYAML = "name: " + typeName + "\ntype: " + jwtType + "\nissuer: https://idp.example.com\nvalidate:\n  jwksUri: https://idp.example.com/jwks\n  allowedSigningAlgs: [RS256]\n"

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
		if c.Method == http.MethodPost {
			return http.StatusCreated, nil, typeJSON
		}
		return http.StatusOK, nil, typeJSON
	})

	p := parse(t, r, "id: "+typeID+"\ncreated_at: 1000\nupdated_at: 2000\n"+minimalYAML)
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	if _, err := r.Create(t.Context(), conn, p); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].Body
	for _, k := range []string{"id", "created_at", "updated_at"} {
		if _, ok := body[k]; ok {
			t.Errorf("%s must be stripped from the request: %v", k, body)
		}
	}
	if body["name"] != typeName || body["issuer"] != "https://idp.example.com" || body["type"] != jwtType {
		t.Errorf("body: %v", body)
	}
}

func TestWriteBodyCarriesTheWholeDefinition(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusCreated, nil, typeJSON })

	text := minimalYAML + "identityMapping:\n  subjectId: sub\n  searchBy: username\n  entityType: agent\nidentityMappingRule: my-rule\n"
	if _, err := r.Create(t.Context(), conn, parse(t, r, text)); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].Body
	im, _ := body["identityMapping"].(map[string]any)
	if im["searchBy"] != "username" || im["entityType"] != "agent" || body["identityMappingRule"] != "my-rule" {
		t.Errorf("identity mapping must be sent: %v", body)
	}
	v, _ := body["validate"].(map[string]any)
	if v["jwksUri"] != "https://idp.example.com/jwks" {
		t.Errorf("validate must be sent: %v", body)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, typeJSON })
	conn.Token = ""

	for name, act := range map[string]func() error{
		"get": func() error { _, err := r.Get(t.Context(), conn, typeID); return err },
		"list": func() error {
			_, err := r.List(t.Context(), conn, resource.ListQuery{})
			return err
		},
		"find": func() error { _, err := r.Find(t.Context(), conn, typeName); return err },
		"create": func() error {
			_, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
			return err
		},
		"replace": func() error {
			_, err := r.Replace(t.Context(), conn, typeID, parse(t, r, minimalYAML))
			return err
		},
		"delete": func() error { return r.Delete(t.Context(), conn, typeID) },
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
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, typeJSON })

	obj, err := r.Get(t.Context(), conn, typeID)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != typeID || obj.Name != typeName {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*oauth2.CustomTokenType)
	if got.Issuer != "https://idp.example.com" || got.IdentityMapping == nil || got.IdentityMapping.SearchBy == nil {
		t.Errorf("value: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/custom_token_types/"+typeID || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":2,"count":2,"page":1,"limit":20,"customTokenTypes":[
		{"id":"id-1","name":"urn:a","type":"` + jwtType + `","issuer":"https://a.example.com","validate":{}},
		{"id":"id-2","name":"urn:b","type":"` + jwtType + `","issuer":"https://b.example.com","validate":{}}
	]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("len: %d", l.Len)
	}
	if first := l.Items.([]*oauth2.CustomTokenType)[0]; first.Name != "urn:a" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "TYPE", "ISSUER", "id-1", "urn:a", jwtType, "https://b.example.com"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/custom_token_types" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{"total":3,"count":2,"page":1,"customTokenTypes":[{"id":"id-1","name":"urn:a","type":"` + jwtType + `","issuer":"i","validate":{}},{"id":"id-2","name":"urn:b","type":"` + jwtType + `","issuer":"i","validate":{}}]}`
	page2 := `{"total":3,"count":1,"page":2,"customTokenTypes":[{"id":"id-3","name":"urn:c","type":"` + jwtType + `","issuer":"i","validate":{}}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "urn:c"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	if res := l.Items.([]*oauth2.CustomTokenType); res[0].Name != "urn:c" {
		t.Errorf("match is not the searched name: %+v", res[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"customTokenTypes":[]}`
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
		return http.StatusOK, nil, `{"total":1,"count":1,"page":1,"customTokenTypes":[` + typeJSON + `]}`
	})

	id, err := r.Find(t.Context(), conn, typeName)
	if err != nil || id != typeID {
		t.Errorf("find existing: id=%q err=%v", id, err)
	}

	_, err = r.Find(t.Context(), conn, "urn:missing")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
}

func TestCreateUsesTheResponseBody(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) { return http.StatusCreated, nil, typeJSON })

	created, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != typeID || created.Name != typeName {
		t.Errorf("created: %+v", created)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/custom_token_types" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
	if len(*calls) != 1 {
		t.Errorf("the 201 carries the new type, so no read-back is needed: %+v", *calls)
	}
}

func TestCreateWithoutBodyFollowsLocation(t *testing.T) {
	r := Resource()
	h := http.Header{}
	h.Set("Location", "/v1.0/custom_token_types/"+typeID)
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, h, ""
		}
		return http.StatusOK, nil, typeJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != typeID || created.Name != typeName {
		t.Errorf("created must be the stored type, with its id: %+v", created)
	}
	got := created.Value.(*oauth2.CustomTokenType)
	if got.CreatedAt == nil || got.UpdatedAt == nil {
		t.Errorf("the stored type has timestamps: %+v", got)
	}
	if len(*calls) != 2 || (*calls)[0].Method != http.MethodPost || (*calls)[1].Method != http.MethodGet || (*calls)[1].Path != "/v1.0/custom_token_types/"+typeID {
		t.Errorf("want a POST then a GET of the Location: %+v", *calls)
	}
}

func TestCreateWithoutBodyOrLocationReturnsTheInput(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusCreated, nil, "" })

	created, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != typeName || created.ID != "" {
		t.Errorf("created: %+v", created)
	}
	if len(*calls) != 1 {
		t.Errorf("there is nothing to read back: %+v", *calls)
	}
}

func TestCreateConflict(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusConflict, nil, `{"error":"FBTAQ5552E","error_description":"already exists"}`
	})
	_, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err == nil || !strings.Contains(err.Error(), "FBTAQ5552E: already exists") {
		t.Errorf("want the 409 translated, got %v", err)
	}
}

func TestReplace(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, ""
		}
		return http.StatusOK, nil, typeJSON
	})

	replaced, err := r.Replace(t.Context(), conn, typeID, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != typeID {
		t.Errorf("replaced id: %+v", replaced)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/custom_token_types/"+typeID {
		t.Errorf("request: %+v", c)
	}
	if g := (*calls)[1]; g.Method != http.MethodGet || g.Path != "/v1.0/custom_token_types/"+typeID {
		t.Errorf("replace must read the result back: %+v", g)
	}
}

func TestReplaceRefusesAnotherID(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, typeJSON })

	_, err := r.Replace(t.Context(), conn, "other-id", parse(t, r, "id: "+typeID+"\n"+minimalYAML))
	if !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("want an invalid-input error, got %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("no request may be sent: %v", *calls)
	}
}

func TestReplaceRejected(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusBadRequest, nil, `{"error":"FBTAQ0010E","error_description":"name differs from the stored value"}`
	})
	_, err := r.Replace(t.Context(), conn, typeID, parse(t, r, minimalYAML))
	if err == nil || !strings.Contains(err.Error(), "FBTAQ0010E: name differs from the stored value") {
		t.Errorf("want the 400 translated, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusNoContent, nil, ""
	})

	if err := r.Delete(t.Context(), conn, typeID); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.Method != http.MethodDelete || c.Path != "/v1.0/custom_token_types/"+typeID || c.Auth != "Bearer tok" {
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
		{http.StatusNotFound, `{"error":"not_found","error_description":"No such type"}`, resource.ErrNotFound, ""},
		{http.StatusForbidden, `{"error":"access_denied","error_description":"Insufficient permissions"}`, nil, "access_denied: Insufficient permissions"},
		{http.StatusInternalServerError, `{"error":"server_error"}`, nil, "server_error"},
		{http.StatusBadGateway, "upstream down", nil, "502"},
	} {
		conn, _ := mock(t, func(call) (int, http.Header, string) {
			return tc.Status, nil, tc.Body
		})
		_, err := r.Get(t.Context(), conn, typeID)
		if tc.Target != nil && !errors.Is(err, tc.Target) {
			t.Errorf("%d: want %v, got %v", tc.Status, tc.Target, err)
		}
		if tc.Text != "" && (err == nil || !strings.Contains(err.Error(), tc.Text)) {
			t.Errorf("%d: error %v should contain %q", tc.Status, err, tc.Text)
		}
	}
}
