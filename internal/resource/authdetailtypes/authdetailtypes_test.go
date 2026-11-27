package authdetailtypes

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

const typeURI = "https://www.example.com/standard"

// typeID is the base64url of typeURI without padding, as the server generates it.
const typeID = "aHR0cHM6Ly93d3cuZXhhbXBsZS5jb20vc3RhbmRhcmQ"

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
		reflect.TypeFor[oauth2.AuthDetailType](),
		reflect.TypeFor[oauth2.AuthDetailTypeWriteRequest](),
		reflect.TypeFor[oauth2.AuthDetailTypeListResponse](),
		reflect.TypeFor[oauth2.Error](),
		reflect.TypeFor[oauth2.ListAuthDetailTypesParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*oauth2.AuthDetailType
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[oauth2.AuthDetailType](&input.Data{Bytes: b})
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
	if v.Type != "vault:path_access" || v.Description == nil || v.Schema == nil || v.SignatureStrategy == nil || v.InlineMode == nil || v.UseCustomMapping == nil {
		t.Errorf("the template should show every kind of property: %+v", v)
	}
	// The schema is a string that must itself be valid JSON, and its "type"
	// must match the type of the template.
	var schema struct {
		Properties struct {
			Type struct {
				Const string `json:"const"`
			} `json:"type"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal([]byte(*v.Schema), &schema); err != nil {
		t.Fatalf("the template schema is not valid JSON: %v", err)
	}
	if schema.Properties.Type.Const != v.Type || len(schema.Required) == 0 {
		t.Errorf("the schema should require its own type %q: %+v", v.Type, schema)
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
		t.Errorf("auth-detail-types missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("auth-detail-types must not offer patch since the API has no PATCH endpoint")
	}
	if r.Name != "auth-detail-types" || r.Singular != "auth-detail-type" || !reflect.DeepEqual(r.Aliases, []string{"auth-detail-type", "auth_detail_types", "auth_detail_type"}) {
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

const typeJSON = `{"id":"` + typeID + `","type":"` + typeURI + `","description":"Payment initiation","signatureStrategy":"full_hash","inlineMode":false,"created_at":1000,"updated_at":2000}`

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

	p := parse(t, r, `id: `+typeID+`
type: `+typeURI+`
description: Payment initiation
created_at: 1000
updated_at: 2000
`)
	if p.Empty() {
		t.Fatal("the type is left, so the payload is not empty")
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
	if body["type"] != typeURI || body["description"] != "Payment initiation" {
		t.Errorf("body: %v", body)
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
		"find": func() error { _, err := r.Find(t.Context(), conn, typeURI); return err },
		"create": func() error {
			_, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\n"))
			return err
		},
		"replace": func() error {
			_, err := r.Replace(t.Context(), conn, typeID, parse(t, r, "type: "+typeURI+"\n"))
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
	if obj.ID != typeID || obj.Name != typeURI {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*oauth2.AuthDetailType)
	if got.Description == nil || *got.Description != "Payment initiation" {
		t.Errorf("description: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/auth_detail_types/"+typeID || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":2,"count":2,"page":1,"authDetailTypes":[
		{"id":"id-1","type":"https://a.example.com/x","description":"first","signatureStrategy":"full_hash"},
		{"id":"id-2","type":"https://b.example.com/y","signatureStrategy":"custom"}
	]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*oauth2.AuthDetailType)[0]
	if first.Type != "https://a.example.com/x" {
		t.Errorf("first: %+v", first)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "DESCRIPTION", "SIGNATURE_STRATEGY", "id-1", "https://a.example.com/x", "first", "custom"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/auth_detail_types" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{"total":3,"count":2,"page":1,"authDetailTypes":[{"id":"id-1","type":"https://a.example.com/x"},{"id":"id-2","type":"https://b.example.com/y"}]}`
	page2 := `{"total":3,"count":1,"page":2,"authDetailTypes":[{"id":"id-3","type":"https://c.example.com/z"}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "https://c.example.com/z"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	res := l.Items.([]*oauth2.AuthDetailType)
	if res[0].Type != "https://c.example.com/z" {
		t.Errorf("match is not the searched type: %+v", res[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"authDetailTypes":[]}`
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
		return http.StatusOK, nil, `{"total":1,"count":1,"page":1,"authDetailTypes":[{"id":"` + typeID + `","type":"` + typeURI + `"}]}`
	})

	id, err := r.Find(t.Context(), conn, typeURI)
	if err != nil || id != typeID {
		t.Errorf("find existing: id=%q err=%v", id, err)
	}

	_, err = r.Find(t.Context(), conn, "https://missing.example.com")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
}

func TestCreate(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusCreated, nil, typeJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\ndescription: Payment initiation\nsignatureStrategy: full_hash\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != typeID || created.Name != typeURI {
		t.Errorf("created: %+v", created)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPost || c.Path != "/v1.0/auth_detail_types" || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
	if c.Body["type"] != typeURI || c.Body["signatureStrategy"] != "full_hash" {
		t.Errorf("body: %v", c.Body)
	}
}

func TestCreateWithoutBodyFollowsLocation(t *testing.T) {
	r := Resource()
	h := http.Header{}
	h.Set("Location", "/v1.0/auth_detail_types/"+typeID)
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, h, ""
		}
		return http.StatusOK, nil, typeJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != typeID || created.Name != typeURI {
		t.Errorf("created must be the stored type, with its id: %+v", created)
	}
	got := created.Value.(*oauth2.AuthDetailType)
	if got.CreatedAt == nil || got.UpdatedAt == nil {
		t.Errorf("the stored type has timestamps: %+v", got)
	}
	if len(*calls) != 2 || (*calls)[0].Method != http.MethodPost || (*calls)[1].Method != http.MethodGet || (*calls)[1].Path != "/v1.0/auth_detail_types/"+typeID {
		t.Errorf("want a POST then a GET of the Location: %+v", *calls)
	}
}

func TestCreateWithoutBodyOrLocationReturnsTheInput(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusCreated, nil, "" })

	created, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Name != typeURI || created.ID != "" {
		t.Errorf("created: %+v", created)
	}
	if len(*calls) != 1 {
		t.Errorf("there is nothing to read back: %+v", *calls)
	}
}

func TestCreateUsesTheResponseBody(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusCreated, nil, typeJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != typeID {
		t.Errorf("created: %+v", created)
	}
	if len(*calls) != 1 {
		t.Errorf("the 201 carries the new type, so no read-back is needed: %+v", *calls)
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

	replaced, err := r.Replace(t.Context(), conn, typeID, parse(t, r, "type: "+typeURI+"\ndescription: Payment initiation\n"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != typeID {
		t.Errorf("replaced id: %+v", replaced)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/auth_detail_types/"+typeID {
		t.Errorf("request: %+v", c)
	}
	if g := (*calls)[1]; g.Method != http.MethodGet {
		t.Errorf("replace must read the result back: %+v", g)
	}
}

func TestReplaceRefusesAnotherID(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, typeJSON })

	_, err := r.Replace(t.Context(), conn, "other-id", parse(t, r, "id: "+typeID+"\ntype: "+typeURI+"\n"))
	if !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("want an invalid-input error, got %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("no request may be sent: %v", *calls)
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
	if c.Method != http.MethodDelete || c.Path != "/v1.0/auth_detail_types/"+typeID || c.Auth != "Bearer tok" {
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

func TestCreateConflict(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusConflict, nil, `{"error":"conflict","error_description":"type already exists"}`
	})
	_, err := r.Create(t.Context(), conn, parse(t, r, "type: "+typeURI+"\n"))
	if err == nil || !strings.Contains(err.Error(), "conflict: type already exists") {
		t.Errorf("want the 409 translated, got %v", err)
	}
}
