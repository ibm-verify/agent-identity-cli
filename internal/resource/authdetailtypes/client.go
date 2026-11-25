package authdetailtypes

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

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(c resource.Conn) (*oauth2.ClientWithResponses, error) {
	d, err := c.Doer()
	if err != nil {
		return nil, err
	}
	return oauth2.NewClientWithResponses(c.Server, oauth2.WithHTTPClient(d))
}

// parseError translates an HTTP status code and error payload to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *oauth2.Error, target string) error {
	switch statusCode {
	case http.StatusUnauthorized:
		return resource.Unauthorized()
	case http.StatusNotFound:
		return resource.NotFound("auth-detail-type", target)
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

// toWriteRequest converts an AuthDetailType to the create/replace request body.
func toWriteRequest(v *oauth2.AuthDetailType) oauth2.AuthDetailTypeWriteRequest {
	return oauth2.AuthDetailTypeWriteRequest{
		Type:              v.Type,
		Description:       v.Description,
		InlineMode:        v.InlineMode,
		Schema:            v.Schema,
		SignatureRule:     v.SignatureRule,
		SignatureStrategy: v.SignatureStrategy,
		UseCustomMapping:  v.UseCustomMapping,
	}
}

// get retrieves an authorization detail type by its ID (or literal type URI).
func get(ctx context.Context, c resource.Conn, id string) (*oauth2.AuthDetailType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetAuthDetailTypeWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON404, resp.JSON500), id)
	}
	return resp.JSON200, nil
}

// list returns authorization detail types. With a search term it walks every
// page and keeps the types whose type URI equals the term; otherwise it asks
// the server for the requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*oauth2.AuthDetailType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	var types []*oauth2.AuthDetailType
	seen := 0
	for page := 1; ; page++ {
		params := &oauth2.ListAuthDetailTypesParams{}
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

		resp, err := client.ListAuthDetailTypesWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var items []oauth2.AuthDetailType
		if resp.JSON200.AuthDetailTypes != nil {
			items = *resp.JSON200.AuthDetailTypes
		}
		for i := range items {
			item := items[i]
			if q.Search != "" && item.Type != q.Search {
				continue
			}
			types = append(types, &item)
		}

		seen += len(items)
		if q.Search == "" || len(items) == 0 {
			break
		}
		if t := resp.JSON200.Total; t != nil && seen >= *t {
			break
		}
	}
	return types, nil
}

// create registers a new authorization detail type and returns it as stored.
func create(ctx context.Context, c resource.Conn, v *oauth2.AuthDetailType) (*oauth2.AuthDetailType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateAuthDetailTypeWithResponse(ctx, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), v.Type)
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}

	// The server answers 201 with no body: the ID is the last segment of Location.
	if resp.Headers201 != nil && resp.Headers201.Location != nil {
		if u, err := url.Parse(*resp.Headers201.Location); err == nil {
			if id := path.Base(u.Path); id != "" && id != "." && id != "/" {
				return get(ctx, c, id)
			}
		}
	}
	return v, nil
}

// replace fully replaces an authorization detail type and returns it as stored.
func replace(ctx context.Context, c resource.Conn, id string, v *oauth2.AuthDetailType) (*oauth2.AuthDetailType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceAuthDetailTypeWithResponse(ctx, id, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON415, resp.JSON500), id)
	}
	// The 200 has no body.
	return get(ctx, c, id)
}

// remove deletes an authorization detail type by its ID (or literal type URI).
func remove(ctx context.Context, c resource.Conn, id string) error {
	client, err := authorize(c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteAuthDetailTypeWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON500), id)
	}
	return nil
}
