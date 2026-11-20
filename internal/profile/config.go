package profile

import (
	"errors"
	"os"

	"go.yaml.in/yaml/v3"
)

// NewConfig returns an empty Config with an initialised context map.
func NewConfig() *Config {
	return &Config{Contexts: map[string]*Environment{}}
}

// Load reads config.yaml. A missing file is not an error and yields an empty
// Config; the file is created on the first Save.
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	cfg := NewConfig()

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*Environment{}
	}
	return cfg, nil
}

// Save atomically writes the Config to config.yaml with 0600 permissions.
func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

type Config struct {
	CurrentContext string                  `yaml:"current-context" json:"current-context"`
	Contexts       map[string]*Environment `yaml:"contexts" json:"contexts"`
}

type Environment struct {
	Server string      `yaml:"server" json:"server"`
	TLS    TLSConfig   `yaml:"tls,omitempty" json:"tls,omitempty"`
	OAuth  OAuthConfig `yaml:"oauth" json:"oauth"`
}

type TLSConfig struct {
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify,omitempty" json:"insecure_skip_verify,omitempty"`
	CACert             Source `yaml:"ca_cert,omitempty" json:"ca_cert,omitempty"`
}

type OAuthConfig struct {
	Issuer       string       `yaml:"issuer" json:"issuer"`
	ClientID     string       `yaml:"client_id" json:"client_id"`
	GrantType    string       `yaml:"grant_type,omitempty" json:"grant_type,omitempty"`
	AuthType     string       `yaml:"auth_type,omitempty" json:"auth_type,omitempty"`
	PrivateJWK   SecretSource `yaml:"private_jwk,omitempty" json:"private_jwk,omitempty"`
	ClientSecret SecretSource `yaml:"client_secret,omitempty" json:"client_secret,omitempty"`
	RedirectURI  string       `yaml:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
	Scopes       []string     `yaml:"scopes,omitempty" json:"scopes,omitempty"`
}
