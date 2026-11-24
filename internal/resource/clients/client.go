package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// authDoer wraps an HttpRequestDoer to inject the Bearer token authorization header.
type authDoer struct {
	client oauth2.HttpRequestDoer
	token  string
}

// Do performs the HTTP request, injecting the Bearer token authorization header first.
func (d *authDoer) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+d.token)
	return d.client.Do(req)
}

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(ctx context.Context, c resource.Conn) (*oauth2.ClientWithResponses, error) {
	if c.Token == "" {
		return nil, resource.Unauthorized()
	}
	var httpClient oauth2.HttpRequestDoer = c.HTTP
	if c.HTTP == nil {
		httpClient = http.DefaultClient
	}
	return oauth2.NewClientWithResponses(c.Server, oauth2.WithHTTPClient(&authDoer{client: httpClient, token: c.Token}))
}

// firstError returns the first non-nil Error payload.
func firstError(errs ...*oauth2.Error) *oauth2.Error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// parseError translates an HTTP status code and OIDC error payload to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *oauth2.Error, target string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return resource.Unauthorized()
	case http.StatusNotFound:
		return resource.NotFound("client", target)
	}
	if apiErr == nil && len(body) > 0 {
		var e oauth2.Error
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

// toWriteRequest converts a Client to the create/replace request body. The two
// types share JSON field names, and read-only fields are dropped by the decode.
func toWriteRequest(v *oauth2.Client) (oauth2.ClientWriteRequest, error) {
	var req oauth2.ClientWriteRequest
	b, err := json.Marshal(v)
	if err != nil {
		return req, err
	}
	err = json.Unmarshal(b, &req)
	return req, err
}

// get retrieves a client by ID.
func get(ctx context.Context, c resource.Conn, id string) (*oauth2.Client, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetClientWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON403, resp.JSON404, resp.JSON500), id)
	}
	return resp.JSON200, nil
}

// list returns clients. With a search term it keeps only the clients whose
// client_name equals the term.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*oauth2.Client, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	var clients []*oauth2.Client
	seen := 0
	for page := 1; ; page++ {
		params := &oauth2.ListClientsParams{
			Page: &page,
		}

		resp, err := client.ListClientsWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var summaries []oauth2.ClientSummary
		if resp.JSON200.Clients != nil {
			summaries = *resp.JSON200.Clients
		}
		for _, s := range summaries {
			if q.Search != "" && (s.ClientName == nil || *s.ClientName != q.Search) {
				continue
			}
			clients = append(clients, &oauth2.Client{
				ClientId:              s.ClientId,
				ClientIdIssuedAt:      s.ClientIdIssuedAt,
				ClientName:            s.ClientName,
				ClientSecretExpiresAt: s.ClientSecretExpiresAt,
				CreatedAt:             s.CreatedAt,
				DefinitionName:        s.DefinitionName,
				Tags:                  s.Tags,
			})
		}

		seen += len(summaries)
		if len(summaries) == 0 {
			break
		}
		if t := resp.JSON200.Total; t != nil && seen >= *t {
			break
		}
	}
	return clients, nil
}

// create registers a new client and returns it as stored by the server.
func create(ctx context.Context, c resource.Conn, v *oauth2.Client) (*oauth2.Client, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	req, err := toWriteRequest(v)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateClientWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		name := ""
		if v.ClientName != nil {
			name = *v.ClientName
		}
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), name)
	}

	// The 201 has no body: the new client's ID is the last segment of Location.
	id := ""
	if resp.Headers201 != nil && resp.Headers201.Location != nil {
		if u, err := url.Parse(*resp.Headers201.Location); err == nil {
			id = path.Base(u.Path)
		}
	}
	if id == "" || id == "." || id == "/" {
		if v.ClientId == nil || *v.ClientId == "" {
			return v, nil
		}
		id = *v.ClientId
	}
	return get(ctx, c, id)
}

// replace fully replaces a client definition and returns it as stored.
func replace(ctx context.Context, c resource.Conn, id string, v *oauth2.Client) (*oauth2.Client, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	req, err := toWriteRequest(v)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceClientWithResponse(ctx, id, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON415, resp.JSON500), id)
	}
	return get(ctx, c, id)
}

// remove deletes a client by ID.
func remove(ctx context.Context, c resource.Conn, id string) error {
	client, err := authorize(ctx, c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteClientWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, firstError(resp.JSON403, resp.JSON500), id)
	}
	return nil
}
