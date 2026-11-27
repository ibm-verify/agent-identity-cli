package keystores

import (
	"strconv"

	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// Keystore is the payload of the keystores resource. The API reads a keystore
// as a summary and writes it as a request with the raw data, so this joins the
// two: what get prints can be sent back to replace. The data and the password
// are write-only and are never returned by the server.
type Keystore struct {
	// KeystoreName is the unique name, the identifier.
	KeystoreName string `json:"keystore_name" yaml:"keystore_name"`
	// KeystoreType is p12 or pem.
	KeystoreType string `json:"keystore_type" yaml:"keystore_type"`
	// KeystoreData is the content: base64 PKCS#12 for p12, a JSON document for pem.
	KeystoreData *string `json:"keystore_data,omitempty" yaml:"keystore_data,omitempty"`
	// KeystorePassword is the passphrase of a p12 keystore.
	KeystorePassword *string `json:"keystore_password,omitempty" yaml:"keystore_password,omitempty"`

	// CreatedAt and UpdatedAt are Unix timestamps in milliseconds, set by the server.
	CreatedAt *int64 `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt *int64 `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

const template = `# Unique name of the keystore. Identifier, immutable. Required.
# Other resources refer to it as "ks:<name>".
keystore_name: my-keystore
# Type of the keystore: p12 or pem. Required.
keystore_type: p12
# Content of the keystore. Write-only, never returned.
# For p12, the base64-encoded PKCS#12 file.
# For pem, a JSON document such as
# {"certificates":[{"label":"ca","content":"-----BEGIN CERTIFICATE-----..."}],
#  "keys":[{"label":"signing","content":"-----BEGIN PRIVATE KEY-----..."}]}
keystore_data: MIIJqQIBAzCCCW8GCSqGSIb3DQEHAaCCCWAEgglc...
# Password of a p12 keystore. Write-only, never returned.
keystore_password: changeit
`

// timestamp formats a millisecond Unix timestamp for a table cell.
func timestamp(t *int64) string {
	if t == nil {
		return ""
	}
	return strconv.FormatInt(*t, 10)
}

// Resource returns the definition of keystores, which is all that cmd.NewRoot
// needs. The API has no PATCH, so the patch verb is not offered.
func Resource() *resource.Resource {
	d := &resource.Def[Keystore]{
		Name:     "keystores",
		Singular: "keystore",
		Aliases:  []string{"keystore", "key-stores", "key-store"},
		Template: template,
		Columns: []resource.Column[Keystore]{
			{Header: "TYPE", Value: func(k *Keystore) string { return k.KeystoreType }},
			{Header: "UPDATED_AT", Value: func(k *Keystore) string { return timestamp(k.UpdatedAt) }},
		},
		ListFlags: resource.ListSupport{Limit: true, Page: true, Search: true},

		ID:     func(k *Keystore) string { return k.KeystoreName },
		NameOf: func(k *Keystore) string { return k.KeystoreName },

		// The server assigns the timestamps, so input that carries them (the
		// output of get) is accepted and they are dropped.
		Strip: func(k *Keystore) {
			k.CreatedAt, k.UpdatedAt = nil, nil
		},
		// The data and password are never returned, so there is nothing to
		// mask in output.

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Delete:  remove,
	}
	return d.Resource()
}
