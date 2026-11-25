package serverconnections

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

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(ctx context.Context, c resource.Conn) (*platform.ClientWithResponses, error) {
	d, err := c.Doer()
	if err != nil {
		return nil, err
	}
	return platform.NewClientWithResponses(c.Server, platform.WithHTTPClient(d))
}

// parseError translates an HTTP status code and error payload to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *platform.Error, target string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return resource.Unauthorized()
	case http.StatusNotFound:
		return resource.NotFound("server-connection", target)
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

// toWriteRequest converts a ServerConnection to the create/replace request body,
// dropping the read-only timestamps.
func toWriteRequest(v *platform.ServerConnection) platform.ServerConnectionWriteRequest {
	return platform.ServerConnectionWriteRequest{
		Http: v.Http,
		Ldap: v.Ldap,
		Name: v.Name,
		Type: v.Type,
	}
}

// get retrieves a server connection by name.
func get(ctx context.Context, c resource.Conn, name string) (*platform.ServerConnection, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetServerConnectionWithResponse(ctx, name)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON404, resp.JSON500), name)
	}
	return resp.JSON200, nil
}

// list returns server connections. With a search term it walks every page and
// keeps the connections whose name equals the term; otherwise it asks the
// server for the requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*platform.ServerConnection, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	var conns []*platform.ServerConnection
	seen := 0
	for page := 1; ; page++ {
		params := &platform.ListServerConnectionsParams{}
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

		resp, err := client.ListServerConnectionsWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var summaries []platform.ServerConnectionSummary
		if resp.JSON200.ServerConnections != nil {
			summaries = *resp.JSON200.ServerConnections
		}
		for _, s := range summaries {
			if q.Search != "" && s.Name != q.Search {
				continue
			}
			conns = append(conns, &platform.ServerConnection{
				Name:      s.Name,
				Type:      s.Type,
				CreatedAt: s.CreatedAt,
				UpdatedAt: s.UpdatedAt,
			})
		}

		seen += len(summaries)
		if q.Search == "" || len(summaries) == 0 {
			break
		}
		if t := resp.JSON200.Total; t != nil && seen >= *t {
			break
		}
	}
	return conns, nil
}

// create registers a new server connection and returns it as stored by the server.
func create(ctx context.Context, c resource.Conn, v *platform.ServerConnection) (*platform.ServerConnection, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateServerConnectionWithResponse(ctx, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), v.Name)
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}

	// No body: the name is the last segment of Location, falling back to the request name.
	name := v.Name
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

// replace fully replaces a server connection definition and returns it as stored.
func replace(ctx context.Context, c resource.Conn, name string, v *platform.ServerConnection) (*platform.ServerConnection, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceServerConnectionWithResponse(ctx, name, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON415, resp.JSON500), name)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return get(ctx, c, name)
}

// remove deletes a server connection by name.
func remove(ctx context.Context, c resource.Conn, name string) error {
	client, err := authorize(ctx, c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteServerConnectionWithResponse(ctx, name)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON404, resp.JSON500), name)
	}
	return nil
}
