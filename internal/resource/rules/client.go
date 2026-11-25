package rules

import (
	"context"
	"net/http"
	"net/url"
	"path"

	"github.com/ibm-verify/agent-identity-cli/internal/client/platform"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// authorize constructs a ClientWithResponses that sends the connection's bearer token.
func authorize(c resource.Conn) (*platform.ClientWithResponses, error) {
	d, err := c.Doer()
	if err != nil {
		return nil, err
	}
	return platform.NewClientWithResponses(c.Server, platform.WithHTTPClient(d))
}

// parseError translates a failed response to a CLI resource error.
func parseError(statusCode int, body []byte, apiErr *platform.Error, target string) error {
	return resource.ParseError(statusCode, body, (*resource.APIError)(apiErr), "rule", target)
}

// fromAPI converts a full rule to the payload type.
func fromAPI(r *platform.Rule) *Rule {
	return &Rule{
		Name:               r.Name,
		Class:              r.Class,
		Source:             r.Source,
		Type:               string(r.Type),
		ImportScriptNames:  r.ImportScriptNames,
		ImportBindingNames: r.ImportBindingNames,
		CreatedAt:          &r.CreatedAt,
		UpdatedAt:          &r.UpdatedAt,
	}
}

// get retrieves a rule by name.
func get(ctx context.Context, c resource.Conn, name string) (*Rule, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetRuleWithResponse(ctx, name)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON404, resp.JSON500), name)
	}
	return fromAPI(resp.JSON200), nil
}

// list returns rule summaries, without their source. With a search term it
// walks every page and keeps the rules whose name equals the term; otherwise
// it asks the server for the requested page.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*Rule, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	var rules []*Rule
	seen := 0
	for page := 1; ; page++ {
		params := &platform.ListRulesParams{}
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

		resp, err := client.ListRulesWithResponse(ctx, params)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode() != http.StatusOK || resp.JSON200 == nil {
			return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON500), "list")
		}

		var summaries []platform.RuleSummary
		if resp.JSON200.Rules != nil {
			summaries = *resp.JSON200.Rules
		}
		for i := range summaries {
			s := summaries[i]
			if q.Search != "" && s.Name != q.Search {
				continue
			}
			rules = append(rules, &Rule{
				Name:               s.Name,
				Class:              s.Class,
				Type:               string(s.Type),
				ImportScriptNames:  s.ImportScriptNames,
				ImportBindingNames: s.ImportBindingNames,
				CreatedAt:          &s.CreatedAt,
				UpdatedAt:          &s.UpdatedAt,
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
	return rules, nil
}

// create registers a new rule and returns it as stored. The server answers
// with no body, so the rule is read back by the name in Location.
func create(ctx context.Context, c resource.Conn, v *Rule) (*Rule, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.CreateRuleWithResponse(ctx, platform.RuleCreateRequest{Name: v.Name, Class: v.Class, Source: v.Source})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusCreated {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON409, resp.JSON415, resp.JSON500), v.Name)
	}

	// The name is the last segment of Location, falling back to the request name.
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

// replace replaces the class and the source of a rule, and returns it as
// stored. The name cannot change, so it is not sent.
func replace(ctx context.Context, c resource.Conn, name string, v *Rule) (*Rule, error) {
	client, err := authorize(c)
	if err != nil {
		return nil, err
	}

	resp, err := client.ReplaceRuleWithResponse(ctx, name, platform.RuleReplaceRequest{Class: v.Class, Source: v.Source})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON415, resp.JSON500), name)
	}
	// The 200 has no body.
	return get(ctx, c, name)
}

// remove deletes a rule by name.
func remove(ctx context.Context, c resource.Conn, name string) error {
	client, err := authorize(c)
	if err != nil {
		return err
	}
	resp, err := client.DeleteRuleWithResponse(ctx, name)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusNoContent {
		return parseError(resp.StatusCode(), resp.Body, resource.FirstError(resp.JSON403, resp.JSON500), name)
	}
	return nil
}
