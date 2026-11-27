package customtokentypes

import (
	"context"
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

// parseError translates a failed response to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *oauth2.Error, target string) error {
	return resource.ParseError(statusCode, body, (*resource.APIError)(apiErr), "custom-token-type", target)
}

// toWriteRequest converts a CustomTokenType to the create/replace request body.
// The ID is derived from the name by the server, so it is not sent.
func toWriteRequest(v *oauth2.CustomTokenType) oauth2.CustomTokenTypeWriteRequest {
	return oauth2.CustomTokenTypeWriteRequest{
		Name:                v.Name,
		Type:                v.Type,
		Issuer:              v.Issuer,
		Validate:            v.Validate,
		IdentityMapping:     v.IdentityMapping,
		IdentityMappingRule: v.IdentityMappingRule,
	}
}

// get retrieves a custom token type by its ID.
func get(ctx context.Context, c resource.Conn, id string) (*oauth2.CustomTokenType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetCustomTokenTypeWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON404, resp.JSON500), id)
	}
	return resp.JSON200, nil
}

// list returns custom token types. With a search term it walks every page and
// keeps the types whose name equals the term; otherwise it asks the server for
// the requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*oauth2.CustomTokenType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	var types []*oauth2.CustomTokenType
	seen := 0
	for page := 1; ; page++ {
		params := &oauth2.ListCustomTokenTypesParams{}
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

		resp, err := client.ListCustomTokenTypesWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var items []oauth2.CustomTokenType
		if resp.JSON200.CustomTokenTypes != nil {
			items = *resp.JSON200.CustomTokenTypes
		}
		for i := range items {
			item := items[i]
			if q.Search != "" && item.Name != q.Search {
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

// create registers a new custom token type and returns it as stored.
func create(ctx context.Context, c resource.Conn, v *oauth2.CustomTokenType) (*oauth2.CustomTokenType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateCustomTokenTypeWithResponse(ctx, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), v.Name)
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

// replace fully replaces a custom token type and returns it as stored. The
// server requires the name and the type to equal the stored values.
func replace(ctx context.Context, c resource.Conn, id string, v *oauth2.CustomTokenType) (*oauth2.CustomTokenType, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceCustomTokenTypeWithResponse(ctx, id, toWriteRequest(v))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON415, resp.JSON500), id)
	}
	// The 200 has no body.
	return get(ctx, c, id)
}

// remove deletes a custom token type by its ID.
func remove(ctx context.Context, c resource.Conn, id string) error {
	client, err := authorize(c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteCustomTokenTypeWithResponse(ctx, id)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON500), id)
	}
	return nil
}
