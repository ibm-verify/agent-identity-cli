// Package i18n localizes user-facing messages using embedded JSON catalogs.
//
// Messages are looked up by stable ID (for example "context.not_found") and
// rendered with named template data. English is the source language and the
// fallback for every other language. See spec.md for the full design.
//
// Nothing is parsed until the first message is rendered, and then only the
// English catalog and the one catalog matching the selected language are
// loaded. A CLI run that never renders a message pays no cost.
package i18n

import (
	"embed"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path"
	"strings"
	"sync"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

// EnvLanguage overrides the language detected from the POSIX locale variables.
const EnvLanguage = "IBM_AGENTID_LANG"

const englishFile = "en.json"

//go:embed locales/*.json
var localeFS embed.FS

// std is the translator behind the package-level functions. Creating it does
// no work.
var std = newTranslator(localeFS, "locales")

// translator lazily loads catalogs from fsys and renders messages.
type translator struct {
	fsys fs.FS
	dir  string

	mu        sync.Mutex
	requested string // normalized tag from SetLanguage; "" means auto-detect
	loc       *goi18n.Localizer
}

func newTranslator(fsys fs.FS, dir string) *translator {
	return &translator{fsys: fsys, dir: dir}
}

// normalizeTag strips the encoding and modifier from a POSIX locale
// ("fr_CA.UTF-8@euro" becomes "fr-CA"). Empty, "C" and "POSIX" become "".
func normalizeTag(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, ".@"); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "_", "-")
	if s == "" || strings.EqualFold(s, "C") || strings.EqualFold(s, "POSIX") {
		return ""
	}
	return s
}

// detectLanguage returns the first usable language setting from the environment.
func detectLanguage() string {
	for _, k := range []string{EnvLanguage, "LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := normalizeTag(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// match picks the catalog for the requested language and returns its tag and
// file name. It only lists the catalog directory; no catalog is parsed. An
// empty or unmatched request selects English without touching the directory.
func (t *translator) match(requested string) (language.Tag, string) {
	if requested == "" {
		return language.English, englishFile
	}
	want, err := language.Parse(requested)
	if err != nil {
		return language.English, englishFile
	}

	entries, err := fs.ReadDir(t.fsys, t.dir)
	if err != nil {
		return language.English, englishFile
	}

	tags := []language.Tag{language.English}
	files := map[language.Tag]string{language.English: englishFile}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || path.Ext(name) != ".json" {
			continue
		}
		tag, err := language.Parse(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		if _, dup := files[tag]; !dup {
			tags = append(tags, tag)
			files[tag] = name
		}
	}

	_, i, conf := language.NewMatcher(tags).Match(want)
	if conf == language.No {
		return language.English, englishFile
	}
	return tags[i], files[tags[i]]
}

// resolve returns the catalog for the explicitly requested language, or for
// the language detected from the environment when none was requested.
func (t *translator) resolve() (language.Tag, string) {
	req := t.requested
	if req == "" {
		req = detectLanguage()
	}
	return t.match(req)
}

func (t *translator) setLanguage(tag string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.requested = normalizeTag(tag)
	t.loc = nil // rebuilt on next use

	matched, _ := t.resolve()
	return matched.String()
}

// localizer builds the localizer on first use, parsing English plus at most
// one other catalog.
func (t *translator) localizer() *goi18n.Localizer {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.loc != nil {
		return t.loc
	}

	matched, file := t.resolve()

	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)

	// A catalog that cannot be read or parsed is skipped: a broken translation
	// must degrade to English, never break the CLI. Tests stop broken embedded
	// catalogs from shipping.
	t.parseInto(b, englishFile)
	if matched != language.English {
		t.parseInto(b, file)
	}

	t.loc = goi18n.NewLocalizer(b, matched.String(), language.English.String())
	return t.loc
}

func (t *translator) parseInto(b *goi18n.Bundle, name string) {
	data, err := fs.ReadFile(t.fsys, path.Join(t.dir, name))
	if err != nil {
		return
	}
	_, _ = b.ParseMessageFileBytes(data, name)
}

// localize renders cfg and never returns an empty string: a message unknown in
// every language yields its ID.
func (t *translator) localize(cfg *goi18n.LocalizeConfig) string {
	// The error only reports that the English fallback was used or that a
	// template field was missing; the returned text is still the best result.
	s, _ := t.localizer().Localize(cfg)
	if s == "" {
		return cfg.MessageID
	}
	return s
}

func (t *translator) t(id string, data map[string]any) string {
	return t.localize(&goi18n.LocalizeConfig{MessageID: id, TemplateData: data})
}

func (t *translator) tn(id string, count int, data map[string]any) string {
	d := maps.Clone(data)
	if d == nil {
		d = map[string]any{}
	}
	d["Count"] = count

	return t.localize(&goi18n.LocalizeConfig{MessageID: id, TemplateData: d, PluralCount: count})
}

// SetLanguage selects the language used by T, TN and localized errors. An
// empty tag auto-detects it from IBM_AGENTID_LANG, LC_ALL, LC_MESSAGES and
// LANG. The tag is matched against the available catalogs, falling back from
// region to base language and finally to English. It returns the language of
// the catalog that matched.
func SetLanguage(tag string) string { return std.setLanguage(tag) }

// T returns the localized message for id, rendered with data. It never
// returns an empty string: an unknown id is returned as is.
func T(id string, data map[string]any) string { return std.t(id, data) }

// TN is T for plural messages. count selects the plural form and is also
// available to the template as {{.Count}}.
func TN(id string, count int, data map[string]any) string { return std.tn(id, count, data) }

// localizedError is an error whose text is rendered when Error is called, so
// the language in effect at print time is used and nothing is loaded for an
// error that is never printed.
type localizedError struct {
	tr    *translator
	id    string
	data  map[string]any
	cause error
}

func (e *localizedError) Error() string { return e.tr.t(e.id, e.data) }

func (e *localizedError) Unwrap() error { return e.cause }

// Errorf returns an error whose text is the localized message id.
func Errorf(id string, data map[string]any) error {
	return &localizedError{tr: std, id: id, data: data}
}

// Wrap is like Errorf and also wraps cause so that errors.Is and errors.As
// match it. The cause's text is not part of the message; pass it in data if
// the message should show it.
func Wrap(cause error, id string, data map[string]any) error {
	return &localizedError{tr: std, id: id, data: data, cause: cause}
}
