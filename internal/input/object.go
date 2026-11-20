package input

import (
	"encoding/json"
	"net/url"

	"go.yaml.in/yaml/v3"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
)

// Decode reads the data as one object of type T, for the resource commands.
// T is normally a generated model. It must carry both json and yaml tags with
// the same names, because YAML is decoded by its own decoder. A resource's
// tests check this by decoding its template in both formats. Unknown
// properties are ignored. A list, a scalar and trailing data are rejected.
func Decode[T any](d *Data) (*T, error) {
	var v T
	if err := decode(d.Bytes, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// TemplateText returns a YAML skeleton as is, or as JSON (without its
// comments) for format "json". An empty format means "yaml".
func TemplateText(yamlText, format string) ([]byte, error) {
	switch format {
	case "", "yaml":
		return []byte(yamlText), nil
	case "json":
		var m map[string]any
		if err := yaml.Unmarshal([]byte(yamlText), &m); err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(b, '\n'), nil
	}
	return nil, i18n.Wrap(ErrInvalidInput, "input.unknown_format", map[string]any{"Format": format})
}

// ValidServer reports whether s is an http or https URL with a host and no
// user name or password.
func ValidServer(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil
}
