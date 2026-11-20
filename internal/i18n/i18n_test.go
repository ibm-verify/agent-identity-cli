package i18n

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

const testEN = `{
  "greet": {"description": "d", "other": "hello {{.Name}}"},
  "only_en": {"description": "d", "other": "english only"},
  "files": {"description": "d", "one": "{{.Count}} file", "other": "{{.Count}} files"}
}`

const testFR = `{
  "greet": {"other": "bonjour {{.Name}}"},
  "files": {"one": "{{.Count}} fichier", "other": "{{.Count}} fichiers"}
}`

const testPTBR = `{"greet": {"other": "olá {{.Name}}"}}`

func testTranslator(t *testing.T, extra map[string]string) *translator {
	t.Helper()
	for _, k := range []string{EnvLanguage, "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}

	fsys := fstest.MapFS{
		"locales/en.json":    {Data: []byte(testEN)},
		"locales/fr.json":    {Data: []byte(testFR)},
		"locales/pt-BR.json": {Data: []byte(testPTBR)},
	}
	for name, data := range extra {
		fsys["locales/"+name] = &fstest.MapFile{Data: []byte(data)}
	}
	return newTranslator(fsys, "locales")
}

func TestNormalizeTag(t *testing.T) {
	tests := map[string]string{
		"":                 "",
		"C":                "",
		"POSIX":            "",
		"en_US.UTF-8":      "en-US",
		"pt_BR":            "pt-BR",
		"fr_CA.UTF-8@euro": "fr-CA",
		" de ":             "de",
	}
	for in, want := range tests {
		if got := normalizeTag(in); got != want {
			t.Errorf("normalizeTag(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLanguageMatching(t *testing.T) {
	tests := []struct {
		tag        string
		wantMatch  string
		wantGreets string
	}{
		{"", "en", "hello Bob"},
		{"en", "en", "hello Bob"},
		{"fr", "fr", "bonjour Bob"},
		{"fr_CA.UTF-8", "fr", "bonjour Bob"}, // region falls back to base language
		{"pt-BR", "pt-BR", "olá Bob"},
		{"de", "en", "hello Bob"},         // unknown language
		{"not a tag!", "en", "hello Bob"}, // unparseable
		{"C", "en", "hello Bob"},
	}
	for _, tt := range tests {
		tr := testTranslator(t, nil)
		if got := tr.setLanguage(tt.tag); got != tt.wantMatch {
			t.Errorf("setLanguage(%q) = %q, want %q", tt.tag, got, tt.wantMatch)
		}
		if got := tr.t("greet", map[string]any{"Name": "Bob"}); got != tt.wantGreets {
			t.Errorf("setLanguage(%q): greet = %q, want %q", tt.tag, got, tt.wantGreets)
		}
	}
}

func TestEnvironmentPrecedence(t *testing.T) {
	tr := testTranslator(t, nil)

	t.Setenv("LANG", "pt_BR.UTF-8")
	if got := tr.setLanguage(""); got != "pt-BR" {
		t.Errorf("LANG: got %q, want pt-BR", got)
	}

	t.Setenv("LC_MESSAGES", "fr_FR.UTF-8")
	if got := tr.setLanguage(""); got != "fr" {
		t.Errorf("LC_MESSAGES should outrank LANG: got %q, want fr", got)
	}

	t.Setenv("LC_ALL", "en_US.UTF-8")
	if got := tr.setLanguage(""); got != "en" {
		t.Errorf("LC_ALL should outrank LC_MESSAGES: got %q, want en", got)
	}

	t.Setenv(EnvLanguage, "fr")
	if got := tr.setLanguage(""); got != "fr" {
		t.Errorf("%s should outrank LC_ALL: got %q, want fr", EnvLanguage, got)
	}

	// An explicit argument beats the environment.
	if got := tr.setLanguage("pt-BR"); got != "pt-BR" {
		t.Errorf("explicit tag: got %q, want pt-BR", got)
	}
}

func TestFallbackToEnglishForMissingTranslation(t *testing.T) {
	tr := testTranslator(t, nil)
	tr.setLanguage("fr")

	if got := tr.t("only_en", nil); got != "english only" {
		t.Errorf("got %q, want the English text", got)
	}
}

func TestUnknownIDReturnsID(t *testing.T) {
	tr := testTranslator(t, nil)
	if got := tr.t("no.such.id", nil); got != "no.such.id" {
		t.Errorf("got %q, want the ID", got)
	}
}

func TestPlurals(t *testing.T) {
	tr := testTranslator(t, nil)

	tests := []struct {
		lang  string
		count int
		want  string
	}{
		{"en", 1, "1 file"},
		{"en", 0, "0 files"},
		{"en", 3, "3 files"},
		{"fr", 1, "1 fichier"},
		{"fr", 0, "0 fichier"}, // French treats 0 as singular
		{"fr", 2, "2 fichiers"},
	}
	for _, tt := range tests {
		tr.setLanguage(tt.lang)
		if got := tr.tn("files", tt.count, nil); got != tt.want {
			t.Errorf("%s count=%d: got %q, want %q", tt.lang, tt.count, got, tt.want)
		}
	}
}

func TestTNDoesNotMutateCallerData(t *testing.T) {
	tr := testTranslator(t, nil)
	data := map[string]any{"Name": "x"}
	tr.tn("files", 2, data)
	if _, ok := data["Count"]; ok {
		t.Error("TN must not modify the caller's map")
	}
}

func TestErrorTranslatesAtRenderTimeAndUnwraps(t *testing.T) {
	tr := testTranslator(t, nil)
	sentinel := errors.New("sentinel")

	err := &localizedError{tr: tr, id: "greet", data: map[string]any{"Name": "Bob"}, cause: sentinel}
	if !errors.Is(err, sentinel) {
		t.Error("errors.Is should find the wrapped sentinel")
	}

	tr.setLanguage("en")
	if got := err.Error(); got != "hello Bob" {
		t.Errorf("english: got %q", got)
	}
	tr.setLanguage("fr")
	if got := err.Error(); got != "bonjour Bob" {
		t.Errorf("french after switching: got %q", got)
	}

	if errors.Unwrap(Errorf("greet", nil)) != nil {
		t.Error("Errorf must not wrap a cause")
	}
	if !errors.Is(Wrap(sentinel, "greet", nil), sentinel) {
		t.Error("Wrap must wrap the cause")
	}
}

func TestLazyLoading(t *testing.T) {
	tr := testTranslator(t, nil)
	if tr.loc != nil {
		t.Fatal("constructing a translator must not load catalogs")
	}
	tr.setLanguage("fr")
	if tr.loc != nil {
		t.Fatal("setLanguage must not load catalogs")
	}
	_ = tr.t("greet", map[string]any{"Name": "x"})
	if tr.loc == nil {
		t.Fatal("rendering a message should load catalogs")
	}
	tr.setLanguage("en")
	if tr.loc != nil {
		t.Fatal("changing the language should drop the loaded catalogs")
	}
}

func TestBrokenCatalogDegradesToEnglish(t *testing.T) {
	tr := testTranslator(t, map[string]string{"de.json": `{not json`})

	if got := tr.setLanguage("de"); got != "de" {
		t.Errorf("de catalog file exists, so it should match: got %q", got)
	}
	if got := tr.t("greet", map[string]any{"Name": "Bob"}); got != "hello Bob" {
		t.Errorf("got %q, want English fallback", got)
	}
}

func TestUnreadableCatalogDirDegradesToEnglish(t *testing.T) {
	tr := newTranslator(fstest.MapFS{"locales/en.json": {Data: []byte(testEN)}}, "missing")
	tr.setLanguage("fr")
	if got := tr.t("greet", map[string]any{"Name": "Bob"}); got != "greet" {
		// No English catalog could be read either, so the ID is the last resort.
		t.Errorf("got %q, want the ID", got)
	}
}

// ---- checks on the embedded catalogs and their use in the source tree ----

var placeholderPattern = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

type entry map[string]string

func loadEmbedded(t *testing.T) map[string]map[string]entry {
	t.Helper()
	files, err := fs.Glob(localeFS, "locales/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no embedded catalogs found: %v", err)
	}

	out := map[string]map[string]entry{}
	for _, f := range files {
		data, err := fs.ReadFile(localeFS, f)
		if err != nil {
			t.Fatal(err)
		}
		var cat map[string]entry
		if err := json.Unmarshal(data, &cat); err != nil {
			t.Fatalf("%s is not valid: %v", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = cat
	}
	if _, ok := out["en"]; !ok {
		t.Fatal("locales/en.json is missing")
	}
	return out
}

func placeholders(e entry) []string {
	var all []string
	for form, text := range e {
		if form == "description" {
			continue
		}
		for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
			all = append(all, m[1])
		}
	}
	slices.Sort(all)
	return slices.Compact(all)
}

func TestEmbeddedCatalogsParse(t *testing.T) {
	loadEmbedded(t)

	// They must also be accepted by go-i18n itself.
	tr := newTranslator(localeFS, "locales")
	for _, tag := range []string{"en"} {
		tr.setLanguage(tag)
		tr.localizer()
	}
}

func TestEnglishEntriesAreComplete(t *testing.T) {
	for id, e := range loadEmbedded(t)["en"] {
		if strings.TrimSpace(e["description"]) == "" {
			t.Errorf("en.json: %q has no description", id)
		}
		if strings.TrimSpace(e["other"]) == "" {
			t.Errorf("en.json: %q has no \"other\" form", id)
		}
	}
}

func TestTranslationParity(t *testing.T) {
	cats := loadEmbedded(t)
	en := cats["en"]

	for tag, cat := range cats {
		if tag == "en" {
			continue
		}
		for id, e := range cat {
			src, ok := en[id]
			if !ok {
				t.Errorf("%s.json: %q does not exist in en.json", tag, id)
				continue
			}
			if want, got := placeholders(src), placeholders(e); !slices.Equal(want, got) {
				t.Errorf("%s.json: %q uses placeholders %v, English uses %v", tag, id, got, want)
			}
		}
	}
}

var (
	callPattern    = regexp.MustCompile(`\bi18n\.(?:T|TN|Errorf|Wrap)\(`)
	literalPattern = regexp.MustCompile(`\bi18n\.(?:(?:T|Errorf)\(\s*|TN\(\s*|Wrap\(\s*[^,]+,\s*)"([^"]+)"`)
)

// usedIDs returns the message IDs referenced from non-test Go files in the
// module, and fails if any call passes an ID that is not a string literal.
func usedIDs(t *testing.T) map[string][]string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root not found at %s: %v", root, err)
	}
	i18nDir := filepath.Join(root, "internal", "i18n")

	used := map[string][]string{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || p == i18nDir {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)

		calls := len(callPattern.FindAll(src, -1))
		lits := literalPattern.FindAllSubmatch(src, -1)
		if calls != len(lits) {
			t.Errorf("%s: %d i18n call(s) but only %d with a string-literal message ID; IDs must be literals", rel, calls, len(lits))
		}
		for _, m := range lits {
			used[string(m[1])] = append(used[string(m[1])], rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return used
}

func TestEveryUsedIDExistsInEnglish(t *testing.T) {
	en := loadEmbedded(t)["en"]
	for id, files := range usedIDs(t) {
		if _, ok := en[id]; !ok {
			t.Errorf("message ID %q (used in %v) is missing from en.json", id, files)
		}
	}
}

func TestNoOrphanedEnglishIDs(t *testing.T) {
	used := usedIDs(t)
	for id := range loadEmbedded(t)["en"] {
		if _, ok := used[id]; !ok {
			t.Errorf("en.json: %q is not used anywhere in the code", id)
		}
	}
}

func TestPackageLevelAPI(t *testing.T) {
	for _, k := range []string{EnvLanguage, "LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(k, "")
	}
	t.Cleanup(func() { SetLanguage("") })

	if got := SetLanguage("en"); got != "en" {
		t.Fatalf("SetLanguage = %q", got)
	}
	got := T("config.home_dir_unresolved", map[string]any{"Error": "boom"})
	if got != "could not determine the home directory: boom" {
		t.Errorf("T = %q", got)
	}
	if got := Errorf("config.home_dir_unresolved", map[string]any{"Error": "boom"}).Error(); got == "" {
		t.Error("Errorf produced an empty message")
	}
}
