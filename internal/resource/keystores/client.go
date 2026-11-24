package keystores

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/ibm-verify/agent-identity-cli/internal/client/platform"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// authDoer wraps an HttpRequestDoer to inject the Bearer token authorization header.
type authDoer struct {
	client platform.HttpRequestDoer
	token  string
}

// Do performs the HTTP request, injecting the Bearer token authorization header first.
func (d *authDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+d.token)
	return d.client.Do(req)
}

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(ctx context.Context, c resource.Conn) (*platform.ClientWithResponses, error) {
	if c.Token == "" {
		return nil, resource.Unauthorized()
	}
	var httpClient platform.HttpRequestDoer = c.HTTP
	if c.HTTP == nil {
		httpClient = http.DefaultClient
	}
	return platform.NewClientWithResponses(c.Server, platform.WithHTTPClient(&authDoer{client: httpClient, token: c.Token}))
}

// firstError returns the first non-nil Error payload.
func firstError(errs ...*platform.Error) *platform.Error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// parseError translates an HTTP status code and error payload to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *platform.Error, target string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return resource.Unauthorized()
	case http.StatusNotFound:
		return resource.NotFound("keystore", target)
	}
	if apiErr == nil && len(body) > 0 {
		var e platform.Error
		if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
			apiErr = &e
		}
	}
	if apiErr != nil && apiErr.Error != "" {
		if apiErr.ErrorDescription != nil && *apiErr.ErrorDescription != "" {
			return fmt.Errorf("%s: %s", apiErr.Error, *apiErr.ErrorDescription)
		}
		return errors.New(apiErr.Error)
	}
	if len(body) > 0 {
		return fmt.Errorf("server returned status code %d: %s", statusCode, string(body))
	}
	return fmt.Errorf("server returned status code %d", statusCode)
}

// fromSummary converts what the server returns on get and list to a Keystore.
// The raw data and password are never returned, so they stay empty.
func fromSummary(s platform.KeystoreSummary) *Keystore {
	k := &Keystore{CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
	if s.KeystoreName != nil {
		k.KeystoreName = *s.KeystoreName
	}
	if s.KeystoreType != nil {
		k.KeystoreType = *s.KeystoreType
	}
	return k
}

// toWriteRequest converts a Keystore to the create/replace request body,
// dropping the read-only timestamps.
func toWriteRequest(k *Keystore) platform.KeystoreWriteRequest {
	return platform.KeystoreWriteRequest{
		KeystoreData:     k.KeystoreData,
		KeystoreName:     k.KeystoreName,
		KeystorePassword: k.KeystorePassword,
		KeystoreType:     platform.KeystoreWriteRequestKeystoreType(k.KeystoreType),
	}
}

// get retrieves a keystore by name.
func get(ctx context.Context, c resource.Conn, name string) (*Keystore, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetKeystoreWithResponse(ctx, name)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON403, resp.JSON404, resp.JSON500), name)
	}
	return fromSummary(*resp.JSON200), nil
}

// list returns keystores. With a search term it walks every page and keeps the
// keystores whose name equals the term; otherwise it asks the server for the
// requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*Keystore, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	var stores []*Keystore
	seen := 0
	for page := 1; ; page++ {
		params := &platform.ListKeystoresParams{}
		if q.Search != "" {
			params.Page = &page
		} else {
			if q.Limit > 0 {
				params.Limit = &q.Limit
			}
			if q.Page > 0 {
				params.Page = &q.Page
			}
		}

		resp, err := client.ListKeystoresWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var summaries []platform.KeystoreSummary
		if resp.JSON200.Keystores != nil {
			summaries = *resp.JSON200.Keystores
		}
		for _, s := range summaries {
			k := fromSummary(s)
			if q.Search != "" && k.KeystoreName != q.Search {
				continue
			}
			stores = append(stores, k)
		}

		seen += len(summaries)
		if q.Search == "" || len(summaries) == 0 {
			break
		}
		if t := resp.JSON200.Total; t != nil && seen >= *t {
			break
		}
	}
	return stores, nil
}

// create registers a new keystore and returns it as stored by the server. The
// API answers with no body, so the keystore is read back.
func create(ctx context.Context, c resource.Conn, v *Keystore) (*Keystore, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateKeystoreWithResponse(ctx, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), v.KeystoreName)
	}

	// The name is the last segment of Location, falling back to the request name.
	name := v.KeystoreName
	if resp.Headers201 != nil && resp.Headers201.Location != nil {
		if u, err := url.Parse(*resp.Headers201.Location); err == nil {
			if base := path.Base(u.Path); base != "" && base != "." && base != "/" {
				name = base
			}
		}
	}
	if name == "" {
		return v, nil
	}
	return get(ctx, c, name)
}

// replace fully replaces a keystore and returns it as stored. The API answers
// with no body, so the keystore is read back.
func replace(ctx context.Context, c resource.Conn, name string, v *Keystore) (*Keystore, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceKeystoreWithResponse(ctx, name, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON415, resp.JSON500), name)
	}
	return get(ctx, c, name)
}

// remove deletes a keystore by name. The API is idempotent: deleting a keystore
// that does not exist succeeds.
func remove(ctx context.Context, c resource.Conn, name string) error {
	client, err := authorize(ctx, c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteKeystoreWithResponse(ctx, name)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON403, resp.JSON500), name)
	}
	return nil
}
