package oauth

import (
	"encoding/json"

	"github.com/go-jose/go-jose/v4"

	"github.com/ibm-verify/agent-identity-cli/internal/i18n"
	"github.com/ibm-verify/agent-identity-cli/internal/profile"
)

// loadKey reads the JWK used for private_key_jwt from a file reference or an
// inline value. The SDK needs a private key with both kid and alg set. Errors
// name the file when there is one, never the key material.
func loadKey(src profile.SecretSource) (*jose.JSONWebKey, error) {
	data, err := src.Read()
	if err != nil {
		return nil, err
	}
	where := keyOrigin(src)

	var jwk jose.JSONWebKey
	if err := json.Unmarshal(data, &jwk); err != nil {
		return nil, i18n.Wrap(err, "auth.key_not_jwk", map[string]any{"Where": where})
	}
	if !jwk.Valid() || jwk.IsPublic() {
		return nil, i18n.Errorf("auth.key_not_private", map[string]any{"Where": where})
	}
	if jwk.KeyID == "" {
		return nil, i18n.Errorf("auth.key_missing_member", map[string]any{"Where": where, "Member": "kid"})
	}
	if jwk.Algorithm == "" {
		return nil, i18n.Errorf("auth.key_missing_member", map[string]any{"Where": where, "Member": "alg"})
	}
	return &jwk, nil
}

// keyOrigin names where a key came from for a message: the file path, or
// "oauth.private_jwk" for an inline value.
func keyOrigin(src profile.SecretSource) string {
	if src.IsFile() {
		return src.Path()
	}
	return "oauth.private_jwk"
}
