package logging

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const redacted = "[REDACTED]"

// sensitiveKeys are the key names (after normalization) whose values are
// always replaced.
var sensitiveKeys = map[string]struct{}{
	"token":               {},
	"access_token":        {},
	"refresh_token":       {},
	"id_token":            {},
	"client_secret":       {},
	"client_assertion":    {},
	"assertion":           {},
	"code":                {},
	"device_code":         {},
	"code_verifier":       {},
	"password":            {},
	"secret":              {},
	"authorization":       {},
	"proxy_authorization": {},
	"cookie":              {},
	"set_cookie":          {},
	"key":                 {},
	"private_key":         {},
	"jwk":                 {},
	"private_jwk":         {},
}

// sensitiveSuffixes also mark a key as sensitive, so "x_api_key" and
// "session_token" match while "token_type" does not.
var sensitiveSuffixes = []string{"_token", "_secret", "_password", "_assertion", "_key"}

// authSchemes are the Authorization schemes that are safe to keep when the
// credential that follows is masked.
var authSchemes = map[string]struct{}{
	"bearer": {}, "basic": {}, "dpop": {}, "digest": {}, "negotiate": {}, "mac": {},
}

var (
	authSchemePattern = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]+`)
	jwtPattern        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]*\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]*`)
	jsonPairPattern   = regexp.MustCompile(`"([A-Za-z0-9_.-]+)"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	kvPairPattern     = regexp.MustCompile(`([A-Za-z0-9_.-]+)=([^&\s"'<>]*)`)
)

// normalizeKey lowercases a key, maps '-' to '_' and drops any group
// qualifier ("oauth.client_secret" becomes "client_secret").
func normalizeKey(k string) string {
	if i := strings.LastIndexByte(k, '.'); i >= 0 {
		k = k[i+1:]
	}
	return strings.ReplaceAll(strings.ToLower(k), "-", "_")
}

// isSensitiveKey reports whether values stored under key must be redacted.
func isSensitiveKey(key string) bool {
	k := normalizeKey(key)
	if _, ok := sensitiveKeys[k]; ok {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(k, suffix) {
			return true
		}
	}
	return false
}

// redactValue masks the value of a sensitive key. Authorization headers keep
// a recognized scheme ("Bearer [REDACTED]") because it is useful and harmless.
func redactValue(key, value string) string {
	switch normalizeKey(key) {
	case "authorization", "proxy_authorization":
		if scheme, _, ok := strings.Cut(strings.TrimSpace(value), " "); ok {
			if _, known := authSchemes[strings.ToLower(scheme)]; known {
				return scheme + " " + redacted
			}
		}
	}
	return redacted
}

// redactText masks credentials in free text by their shape: Bearer and Basic
// values, JWTs, and key=value or "key":"value" pairs with a sensitive key.
func redactText(s string) string {
	if s == "" {
		return s
	}

	s = authSchemePattern.ReplaceAllString(s, "$1 "+redacted)
	s = jwtPattern.ReplaceAllString(s, "[JWT REDACTED]")

	s = jsonPairPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := jsonPairPattern.FindStringSubmatch(m)
		if isSensitiveKey(sub[1]) {
			return `"` + sub[1] + `":"` + redacted + `"`
		}
		return m
	})

	return kvPairPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := kvPairPattern.FindStringSubmatch(m)
		if isSensitiveKey(sub[1]) {
			return sub[1] + "=" + redacted
		}
		return m
	})
}

// redactForm redacts a form-encoded body or a URL query string. The result is
// sorted by key and is meant for reading, not for sending.
func redactForm(raw string) string {
	if raw == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return redactText(raw)
	}

	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	var parts []string
	for _, k := range keys {
		for _, v := range values[k] {
			if isSensitiveKey(k) {
				v = redacted
			} else {
				v = url.QueryEscape(redactText(v))
			}
			parts = append(parts, url.QueryEscape(k)+"="+v)
		}
	}
	return strings.Join(parts, "&")
}

// redactJSON redacts every value stored under a sensitive key, at any depth,
// and masks credential-shaped strings elsewhere. It reports false when data is
// not a single valid JSON document.
func redactJSON(data []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	var v any
	if err := dec.Decode(&v); err != nil {
		return "", false
	}
	if _, err := dec.Token(); err != io.EOF {
		return "", false // trailing content
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(redactTree(v)); err != nil {
		return "", false
	}
	return strings.TrimSuffix(buf.String(), "\n"), true
}

func redactTree(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if isSensitiveKey(k) {
				t[k] = redacted
			} else {
				t[k] = redactTree(val)
			}
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = redactTree(val)
		}
		return t
	case string:
		return redactText(t)
	}
	return v
}

// replaceAttr is the slog.HandlerOptions.ReplaceAttr that applies redaction to
// every record, so call sites cannot forget it.
func replaceAttr(groups []string, a slog.Attr) slog.Attr {
	if len(groups) == 0 {
		switch a.Key {
		case slog.TimeKey, slog.LevelKey:
			return a
		case slog.MessageKey:
			return slog.String(a.Key, redactText(a.Value.String()))
		}
	}

	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		return a
	}

	if isSensitiveKey(a.Key) {
		if v.Kind() == slog.KindString {
			return slog.String(a.Key, redactValue(a.Key, v.String()))
		}
		return slog.String(a.Key, redacted)
	}

	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactText(v.String()))
	case slog.KindAny:
		// Error text can embed URLs and other credential-shaped values.
		if err, ok := v.Any().(error); ok {
			return slog.String(a.Key, redactText(err.Error()))
		}
	}
	return a
}
