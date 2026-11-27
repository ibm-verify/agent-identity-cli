package oauthproviders

import (
	"context"
	"net/http"

	"github.com/ibm-verify/agent-identity-cli/internal/client/oauth2"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(ctx context.Context, c resource.Conn) (*oauth2.ClientWithResponses, error) {
	d, err := c.Doer()
	if err != nil {
		return nil, err
	}
	return oauth2.NewClientWithResponses(c.Server, oauth2.WithHTTPClient(d))
}

// parseError translates a failed response to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *oauth2.Error, target string) error {
	return resource.ParseError(statusCode, body, (*resource.APIError)(apiErr), "oauth-provider", target)
}

// toWriteRequest converts an OAuthProviderResponse to the create/replace request body.
func toWriteRequest(v *oauth2.OAuthProviderResponse) oauth2.OAuthProviderWriteRequest {
	name := v.DefinitionName
	return oauth2.OAuthProviderWriteRequest{
		DefinitionName:  &name,
		Issuer:          v.Issuer,
		JwksSigKeystore: v.JwksSigKeystore,
		JwksEncKeystore: v.JwksEncKeystore,
		TokenSettings:   v.TokenSettings,
		DcrSettings:     v.DcrSettings,
	}
}

// get retrieves an OAuth provider by definition name.
func get(ctx context.Context, c resource.Conn, name string) (*oauth2.OAuthProviderResponse, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetOAuthProviderWithResponse(ctx, name)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON404, resp.JSON500), name)
	}
	return resp.JSON200, nil
}

// list returns OAuth providers. With a search term it walks every page and keeps
// the providers whose definition_name equals the term; otherwise it asks the
// server for the requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*oauth2.OAuthProviderResponse, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	var providers []*oauth2.OAuthProviderResponse
	seen := 0
	for page := 1; ; page++ {
		params := &oauth2.ListOAuthProvidersParams{}
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

		resp, err := client.ListOAuthProvidersWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON500), "list")
		}

		var items []oauth2.OAuthProviderResponse
		if resp.JSON200.OauthProviders != nil {
			items = *resp.JSON200.OauthProviders
		}
		for i := range items {
			item := items[i]
			if q.Search != "" && item.DefinitionName != q.Search {
				continue
			}
			providers = append(providers, &item)
		}

		seen += len(items)
		if q.Search == "" || len(items) == 0 {
			break
		}
		if t := resp.JSON200.Total; t != nil && seen >= *t {
			break
		}
	}
	return providers, nil
}

// replace fully replaces an OAuth provider definition and returns it as stored.
func replace(ctx context.Context, c resource.Conn, name string, v *oauth2.OAuthProviderResponse) (*oauth2.OAuthProviderResponse, error) {
	client, err := authorize(ctx, c)
	if err != nil {
		return nil, err
	}

	req := toWriteRequest(v)
	resp, err := client.ReplaceOAuthProviderWithResponse(ctx, name, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON404, resp.JSON415, resp.JSON500), name)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return get(ctx, c, name)
}
