package rules

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

const ruleName = "agent-cc-premapping"

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
		reflect.TypeFor[Rule](),
		reflect.TypeFor[platform.Rule](),
		reflect.TypeFor[platform.RuleSummary](),
		reflect.TypeFor[platform.RuleCreateRequest](),
		reflect.TypeFor[platform.RuleReplaceRequest](),
		reflect.TypeFor[platform.RuleListResponse](),
		reflect.TypeFor[platform.Error](),
		reflect.TypeFor[platform.ListRulesParams](),
	} {
		check(typ)
	}
}

func TestTemplateDecodesInBothFormats(t *testing.T) {
	r := Resource()

	var got []*Rule
	for _, format := range []string{"yaml", "json"} {
		b, err := r.Template(format)
		if err != nil {
			t.Fatal(err)
		}
		v, err := input.Decode[Rule](&input.Data{Bytes: b})
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
	if v.Name != ruleName || v.Class != platform.RuleClassMappingRule {
		t.Errorf("the template should carry the required properties: %+v", v)
	}
	// The source is multi-line JavaScript and must keep its line breaks.
	if strings.Count(v.Source, "\n") < 2 || !strings.Contains(v.Source, `tokenData["custom_claim"]`) {
		t.Errorf("the template should show multi-line JavaScript: %q", v.Source)
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
		t.Errorf("rules missing expected verbs: %+v", r)
	}
	if r.Patch != nil {
		t.Error("rules must not offer patch since the API has no PATCH endpoint")
	}
	if r.Name != "rules" || r.Singular != "rule" || !reflect.DeepEqual(r.Aliases, []string{"rule"}) {
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

const ruleJSON = `{"name":"` + ruleName + `","class":"mappingrule","type":"javascript","source":"tokenData[\"a\"] = 1;\ntokenData[\"b\"] = 2;\n","import_script_names":["other"],"import_binding_names":[],"created_at":1000,"updated_at":2000}`

const minimalYAML = "name: " + ruleName + "\nclass: mappingrule\nsource: |\n  tokenData[\"a\"] = 1;\n  tokenData[\"b\"] = 2;\n"

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
			return http.StatusCreated, nil, ""
		}
		return http.StatusOK, nil, ruleJSON
	})

	p := parse(t, r, minimalYAML+"type: javascript\nimport_script_names: [other]\nimport_binding_names: []\ncreated_at: 1000\nupdated_at: 2000\n")
	if p.Empty() {
		t.Fatal("the name is left, so the payload is not empty")
	}
	if _, err := r.Create(t.Context(), conn, p); err != nil {
		t.Fatal(err)
	}
	body := (*calls)[0].Body
	if len(body) != 3 || body["name"] != ruleName || body["class"] != "mappingrule" || body["source"] == "" {
		t.Errorf("only the name, class and source are sent: %v", body)
	}
}

func TestServerNeedsAToken(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, ruleJSON })
	conn.Token = ""

	for name, act := range map[string]func() error{
		"get": func() error { _, err := r.Get(t.Context(), conn, ruleName); return err },
		"list": func() error {
			_, err := r.List(t.Context(), conn, resource.ListQuery{})
			return err
		},
		"find": func() error { _, err := r.Find(t.Context(), conn, ruleName); return err },
		"create": func() error {
			_, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
			return err
		},
		"replace": func() error {
			_, err := r.Replace(t.Context(), conn, ruleName, parse(t, r, minimalYAML))
			return err
		},
		"delete": func() error { return r.Delete(t.Context(), conn, ruleName) },
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
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, ruleJSON })

	obj, err := r.Get(t.Context(), conn, ruleName)
	if err != nil {
		t.Fatal(err)
	}
	if obj.ID != ruleName || obj.Name != ruleName {
		t.Errorf("object: %+v", obj)
	}
	got := obj.Value.(*Rule)
	if got.Class != platform.RuleClassMappingRule || got.Type != "javascript" || !strings.Contains(got.Source, "tokenData") ||
		len(got.ImportScriptNames) != 1 || got.UpdatedAt == nil || *got.UpdatedAt != 2000 {
		t.Errorf("value: %+v", got)
	}
	c := (*calls)[0]
	if c.Method != http.MethodGet || c.Path != "/v1.0/rules/"+ruleName || c.Auth != "Bearer tok" {
		t.Errorf("request: %+v", c)
	}
}

func TestList(t *testing.T) {
	r := Resource()
	items := `{"total":2,"count":2,"page":1,"limit":20,"rules":[
		{"name":"rule-1","class":"mappingrule","type":"javascript","import_script_names":[],"import_binding_names":[],"created_at":1,"updated_at":10},
		{"name":"rule-2","class":"accesspolicy","type":"javascript","import_script_names":[],"import_binding_names":[],"created_at":2,"updated_at":20}
	]}`
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, items })

	l, err := r.List(t.Context(), conn, resource.ListQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 2 {
		t.Fatalf("len: %d", l.Len)
	}
	first := l.Items.([]*Rule)[0]
	if first.Name != "rule-1" || first.Source != "" {
		t.Errorf("a list has no source: %+v", first)
	}
	// An absent source must not print as an empty key.
	b, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"source"`) {
		t.Errorf("a list item must leave out the source: %s", b)
	}
	var sb strings.Builder
	if err := l.WriteTable(&sb); err != nil {
		t.Fatal(err)
	}
	table := sb.String()
	for _, want := range []string{"ID", "NAME", "CLASS", "UPDATED_AT", "rule-1", "mappingrule", "accesspolicy", "20"} {
		if !strings.Contains(table, want) {
			t.Errorf("table lacks %q:\n%s", want, table)
		}
	}
	if c := (*calls)[0]; c.Method != http.MethodGet || c.Path != "/v1.0/rules" || len(c.Query["limit"]) != 0 || len(c.Query["page"]) != 0 {
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
	page1 := `{"total":3,"count":2,"page":1,"rules":[{"name":"rule-a","class":"mappingrule","type":"javascript"},{"name":"rule-b","class":"mappingrule","type":"javascript"}]}`
	page2 := `{"total":3,"count":1,"page":2,"rules":[{"name":"rule-c","class":"accesspolicy","type":"javascript"}]}`
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Query.Get("page") == "2" {
			return http.StatusOK, nil, page2
		}
		return http.StatusOK, nil, page1
	})

	l, err := r.List(t.Context(), conn, resource.ListQuery{Search: "rule-c"})
	if err != nil {
		t.Fatal(err)
	}
	if l.Len != 1 {
		t.Fatalf("expected 1 match, got %d", l.Len)
	}
	if res := l.Items.([]*Rule); res[0].Name != "rule-c" || res[0].Class != platform.RuleClassAccessPolicy {
		t.Errorf("match is not the searched name: %+v", res[0])
	}
	if len(*calls) != 2 {
		t.Fatalf("expected 2 page requests, got %d", len(*calls))
	}
}

func TestListEmpty(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusOK, nil, `{"total":0,"count":0,"rules":[]}`
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
		return http.StatusOK, nil, `{"total":1,"count":1,"page":1,"rules":[{"name":"` + ruleName + `","class":"mappingrule","type":"javascript"}]}`
	})

	id, err := r.Find(t.Context(), conn, ruleName)
	if err != nil || id != ruleName {
		t.Errorf("find existing: id=%q err=%v", id, err)
	}

	_, err = r.Find(t.Context(), conn, "missing")
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("find missing: %v", err)
	}
}

func TestCreateReadsTheRuleBackFromLocation(t *testing.T) {
	r := Resource()
	h := http.Header{}
	h.Set("Location", "/v1.0/rules/"+ruleName)
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, h, ""
		}
		return http.StatusOK, nil, ruleJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != ruleName || created.Name != ruleName {
		t.Errorf("created: %+v", created)
	}
	got := created.Value.(*Rule)
	if got.Type != "javascript" || got.CreatedAt == nil || got.UpdatedAt == nil {
		t.Errorf("the stored rule has the derived type and the timestamps: %+v", got)
	}
	if len(*calls) != 2 || (*calls)[0].Method != http.MethodPost || (*calls)[0].Path != "/v1.0/rules" ||
		(*calls)[1].Method != http.MethodGet || (*calls)[1].Path != "/v1.0/rules/"+ruleName {
		t.Errorf("want a POST then a GET of the Location: %+v", *calls)
	}
	body := (*calls)[0].Body
	if body["name"] != ruleName || body["class"] != "mappingrule" || !strings.Contains(body["source"].(string), "tokenData") {
		t.Errorf("body: %v", body)
	}
	if (*calls)[0].Auth != "Bearer tok" {
		t.Errorf("auth: %q", (*calls)[0].Auth)
	}
}

func TestCreateWithoutLocationReadsBackByName(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPost {
			return http.StatusCreated, nil, ""
		}
		return http.StatusOK, nil, ruleJSON
	})

	created, err := r.Create(t.Context(), conn, parse(t, r, minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != ruleName {
		t.Errorf("created: %+v", created)
	}
	if len(*calls) != 2 || (*calls)[1].Path != "/v1.0/rules/"+ruleName {
		t.Errorf("want a POST then a GET of the request name: %+v", *calls)
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
		return http.StatusOK, nil, ruleJSON
	})

	replaced, err := r.Replace(t.Context(), conn, ruleName, parse(t, r, "name: "+ruleName+"\nclass: accesspolicy\nsource: \"true;\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if replaced.ID != ruleName {
		t.Errorf("replaced: %+v", replaced)
	}
	c := (*calls)[0]
	if c.Method != http.MethodPut || c.Path != "/v1.0/rules/"+ruleName {
		t.Errorf("request: %+v", c)
	}
	// The name cannot change, so it is not in the body.
	if len(c.Body) != 2 || c.Body["class"] != "accesspolicy" || c.Body["source"] != "true;" {
		t.Errorf("body must carry only the class and the source: %v", c.Body)
	}
	if g := (*calls)[1]; g.Method != http.MethodGet || g.Path != "/v1.0/rules/"+ruleName {
		t.Errorf("replace must read the result back: %+v", g)
	}
}

func TestReplaceWithoutNameInPayload(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		if c.Method == http.MethodPut {
			return http.StatusOK, nil, ""
		}
		return http.StatusOK, nil, ruleJSON
	})

	if _, err := r.Replace(t.Context(), conn, ruleName, parse(t, r, "class: mappingrule\nsource: \"1;\"\n")); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].Path != "/v1.0/rules/"+ruleName {
		t.Errorf("the target comes from the command, not the payload: %+v", (*calls)[0])
	}
}

func TestReplaceRefusesAnotherName(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(call) (int, http.Header, string) { return http.StatusOK, nil, ruleJSON })

	_, err := r.Replace(t.Context(), conn, "other-rule", parse(t, r, minimalYAML))
	if !errors.Is(err, input.ErrInvalidInput) {
		t.Errorf("want an invalid-input error, got %v", err)
	}
	if len(*calls) != 0 {
		t.Errorf("no request may be sent: %v", *calls)
	}
}

func TestReplaceNotFound(t *testing.T) {
	r := Resource()
	conn, _ := mock(t, func(call) (int, http.Header, string) {
		return http.StatusNotFound, nil, `{"error":"not_found"}`
	})
	_, err := r.Replace(t.Context(), conn, ruleName, parse(t, r, minimalYAML))
	if !errors.Is(err, resource.ErrNotFound) {
		t.Errorf("want not found, got %v", err)
	}
}

func TestRemove(t *testing.T) {
	r := Resource()
	conn, calls := mock(t, func(c call) (int, http.Header, string) {
		return http.StatusNoContent, nil, ""
	})

	if err := r.Delete(t.Context(), conn, ruleName); err != nil {
		t.Fatal(err)
	}
	c := (*calls)[0]
	if c.Method != http.MethodDelete || c.Path != "/v1.0/rules/"+ruleName || c.Auth != "Bearer tok" {
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
		{http.StatusNotFound, `{"error":"not_found","error_description":"No such rule"}`, resource.ErrNotFound, ""},
		{http.StatusForbidden, `{"error":"access_denied","error_description":"Insufficient permissions"}`, nil, "access_denied: Insufficient permissions"},
		{http.StatusInternalServerError, `{"error":"server_error"}`, nil, "server_error"},
		{http.StatusBadGateway, "upstream down", nil, "502"},
	} {
		conn, _ := mock(t, func(call) (int, http.Header, string) {
			return tc.Status, nil, tc.Body
		})
		_, err := r.Get(t.Context(), conn, ruleName)
		if tc.Target != nil && !errors.Is(err, tc.Target) {
			t.Errorf("%d: want %v, got %v", tc.Status, tc.Target, err)
		}
		if tc.Text != "" && (err == nil || !strings.Contains(err.Error(), tc.Text)) {
			t.Errorf("%d: error %v should contain %q", tc.Status, err, tc.Text)
		}
	}
}
