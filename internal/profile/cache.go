package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	// expirySkew is how long before expires_at a token already counts as expired.
	expirySkew = 30 * time.Second

	defaultGrantType = "authorization_code"
	defaultTokenType = "Bearer"
	cacheFileExt     = ".json"
)

// Token is the cached token state for one context (cache/<context>.json).
type Token struct {
	ConfigHash   string    `json:"config_hash"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"` // zero means the expiry is unknown
}

// Status is the result of judging a cached token against its context.
type Status int

const (
	TokenMissing Status = iota // no file, unreadable, or corrupt
	TokenStale                 // config_hash does not match the context
	TokenExpired               // matches, but past expiry
	TokenValid                 // matches and still usable
)

// NewToken stamps a token response for env. expiresIn is the "expires_in"
// value in seconds and now is the time the response was received. A zero or
// negative expiresIn leaves ExpiresAt zero, meaning the expiry is unknown.
func NewToken(env *Environment, accessToken, refreshToken, tokenType string, expiresIn int64, now time.Time) *Token {
	t := &Token{
		ConfigHash:   ConfigHash(env),
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenType,
	}
	if t.TokenType == "" {
		t.TokenType = defaultTokenType
	}
	if expiresIn > 0 {
		t.ExpiresAt = now.Add(time.Duration(expiresIn) * time.Second).UTC()
	}
	return t
}

// Expired reports whether the token is within the expiry skew of ExpiresAt or
// past it. A token with no known expiry is never expired.
func (t *Token) Expired(now time.Time) bool {
	if t.ExpiresAt.IsZero() {
		return false
	}
	return !now.Before(t.ExpiresAt.Add(-expirySkew))
}

// ConfigHash returns a digest of the context fields that decide what a token is
// for: server, issuer, client ID, grant type and scopes. Scope order and
// duplicates do not matter, and an omitted grant type equals the default.
func ConfigHash(env *Environment) string {
	if env == nil {
		env = &Environment{}
	}

	grant := env.OAuth.GrantType
	if grant == "" {
		grant = defaultGrantType
	}

	scopes := slices.Clone(env.OAuth.Scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	if scopes == nil {
		scopes = []string{}
	}

	// A JSON structure keeps one field's value from running into the next.
	data, _ := json.Marshal([]any{env.Server, env.OAuth.Issuer, env.OAuth.ClientID, grant, scopes})
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TokenPath returns the path of the cache file for the named context.
func TokenPath(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+cacheFileExt), nil
}

// SaveToken atomically writes the token to the context's cache file with 0600
// permissions.
func SaveToken(name string, t *Token) error {
	path, err := TokenPath(name)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// LookupToken reads the cache for the named context and judges it against env.
// File problems are not errors: they yield TokenMissing, and the caller mints or
// asks for a new login. The returned *Token is nil for TokenMissing and
// TokenStale. An error is returned only for an invalid name or an unresolvable
// state directory.
func LookupToken(name string, env *Environment, now time.Time) (*Token, Status, error) {
	path, err := TokenPath(name)
	if err != nil {
		return nil, TokenMissing, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			slog.Debug("token cache miss", "context", name, "path", path, "reason", "no file")
		} else {
			slog.Warn("token cache unreadable", "context", name, "path", path, "error", err)
		}
		return nil, TokenMissing, nil
	}

	var t Token
	if err := json.Unmarshal(data, &t); err != nil || t.AccessToken == "" {
		slog.Warn("token cache corrupt", "context", name, "path", path)
		return nil, TokenMissing, nil
	}

	if t.ConfigHash != ConfigHash(env) {
		slog.Warn("token cache ignored: context settings changed", "context", name, "path", path)
		return nil, TokenStale, nil
	}

	if t.Expired(now) {
		slog.Debug("token cache expired", "context", name, "has_refresh_token", t.RefreshToken != "")
		return &t, TokenExpired, nil
	}

	slog.Debug("token cache hit", "context", name)
	return &t, TokenValid, nil
}

// DeleteToken removes the context's cache file. A missing file is not an error.
func DeleteToken(name string) error {
	path, err := TokenPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// DeleteAllTokens removes every *.json file in the cache directory. A missing
// directory is not an error, and other files are left alone.
func DeleteAllTokens() error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), cacheFileExt) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// RenameToken moves the cache file of oldName to newName, replacing any file
// already there. A missing source is not an error.
func RenameToken(oldName, newName string) error {
	oldPath, err := TokenPath(oldName)
	if err != nil {
		return err
	}
	newPath, err := TokenPath(newName)
	if err != nil {
		return err
	}
	if oldPath == newPath {
		return nil
	}
	if err := os.Rename(oldPath, newPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
