package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ibm-verify/agent-identity-cli/internal/client/registry"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

// newClient constructs a ClientWithResponses that sends the connection's bearer token.
func newClient(ctx context.Context, c resource.Conn) (*registry.ClientWithResponses, error) {
	d, err := c.Doer()
	if err != nil {
		return nil, err
	}
	return registry.NewClientWithResponses(c.Server, registry.WithHTTPClient(d))
}

// parseError translates standard SCIM error payloads and HTTP status codes to CLI resource errors.
func parseError(statusCode int, body []byte, scimErr *registry.SCIMError, target string) error {
	if statusCode == http.StatusUnauthorized {
		return resource.Unauthorized()
	}
	if statusCode == http.StatusNotFound {
		return resource.NotFound("agent", target)
	}
	if scimErr != nil && scimErr.Detail != "" {
		return errors.New(scimErr.Detail)
	}
	if len(body) > 0 {
		var se registry.SCIMError
		if err := json.Unmarshal(body, &se); err == nil && se.Detail != "" {
			return errors.New(se.Detail)
		}
		return fmt.Errorf("server returned status code %d: %s", statusCode, string(body))
	}
	return fmt.Errorf("server returned status code %d", statusCode)
}

// get retrieves a specific agent by ID.
func get(ctx context.Context, c resource.Conn, id string) (*registry.Agent, error) {
	client, err := newClient(ctx, c)
	if err != nil {
		return nil, err
	}
	resp, err := client.GetV10AgentsIdWithResponse(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	if resp.HTTPResponse.StatusCode != http.StatusOK {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON400 != nil {
			scimErr = resp.ApplicationscimJSON400
		} else if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON404 != nil {
			scimErr = resp.ApplicationscimJSON404
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return nil, parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, id)
	}
	return resp.ApplicationscimJSON200, nil
}

// list filters agents.
func list(ctx context.Context, c resource.Conn, q resource.ListQuery) ([]*registry.Agent, error) {
	client, err := newClient(ctx, c)
	if err != nil {
		return nil, err
	}

	attrs := "status,externalIds,displayName,description,owners,tags,meta"
	params := &registry.GetV10AgentsParams{
		Attributes: &attrs,
	}
	if q.Search != "" {
		filter := fmt.Sprintf("displayName eq %q", q.Search)
		params.Filter = &filter
	}

	resp, err := client.GetV10AgentsWithResponse(ctx, params)
	if err != nil {
		return nil, err
	}

	if resp.HTTPResponse.StatusCode != http.StatusOK {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON400 != nil {
			scimErr = resp.ApplicationscimJSON400
		} else if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return nil, parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, "list")
	}

	var agents []*registry.Agent
	if resp.ApplicationscimJSON200 != nil && resp.ApplicationscimJSON200.Resources != nil {
		for _, item := range *resp.ApplicationscimJSON200.Resources {
			var status *registry.AgentStatus
			if item.Status != nil {
				s := registry.AgentStatus(*item.Status)
				status = &s
			}
			idStr := item.Id
			agents = append(agents, &registry.Agent{
				Id:          &idStr,
				DisplayName: item.DisplayName,
				Description: item.Description,
				Status:      status,
				Tags:        item.Tags,
				ExternalIds: item.ExternalIds,
				Owners:      item.Owners,
				Meta:        item.Meta,
				Schemas:     item.Schemas,
			})
		}
	}

	return agents, nil
}

// create onboards a new agent.
func create(ctx context.Context, c resource.Conn, v *registry.Agent) (*registry.Agent, error) {
	client, err := newClient(ctx, c)
	if err != nil {
		return nil, err
	}

	req := registry.CreateAgentRequest{
		Description: v.Description,
		DisplayName: v.DisplayName,
		ExternalIds: v.ExternalIds,
		Owners:      v.Owners,
		Schemas:     v.Schemas,
		Tags:        v.Tags,
	}
	if len(req.Schemas) == 0 {
		req.Schemas = []string{"urn:ietf:params:scim:schemas:core:ibm:2.0:Agent"}
	}
	if v.Status != nil {
		s := registry.CreateAgentRequestStatus(*v.Status)
		req.Status = &s
	}

	resp, err := client.PostV10AgentsWithApplicationScimPlusJSONBodyWithResponse(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.HTTPResponse.StatusCode != http.StatusCreated && resp.HTTPResponse.StatusCode != http.StatusOK {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON400 != nil {
			scimErr = resp.ApplicationscimJSON400
		} else if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON409 != nil {
			scimErr = resp.ApplicationscimJSON409
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return nil, parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, v.DisplayName)
	}

	return resp.ApplicationscimJSON201, nil
}

// replace fully replaces an agent definition.
func replace(ctx context.Context, c resource.Conn, id string, v *registry.Agent) (*registry.Agent, error) {
	client, err := newClient(ctx, c)
	if err != nil {
		return nil, err
	}

	req := registry.UpdateAgentRequest{
		Description: v.Description,
		DisplayName: v.DisplayName,
		ExternalIds: v.ExternalIds,
		Owners:      v.Owners,
		Schemas:     v.Schemas,
		Tags:        v.Tags,
	}
	if len(req.Schemas) == 0 {
		req.Schemas = []string{"urn:ietf:params:scim:schemas:core:ibm:2.0:Agent"}
	}
	if v.Status != nil {
		s := registry.UpdateAgentRequestStatus(*v.Status)
		req.Status = &s
	}

	resp, err := client.PutV10AgentsIdWithApplicationScimPlusJSONBodyWithResponse(ctx, id, req)
	if err != nil {
		return nil, err
	}

	if resp.HTTPResponse.StatusCode != http.StatusOK {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON400 != nil {
			scimErr = resp.ApplicationscimJSON400
		} else if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON404 != nil {
			scimErr = resp.ApplicationscimJSON404
		} else if resp.ApplicationscimJSON409 != nil {
			scimErr = resp.ApplicationscimJSON409
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return nil, parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, id)
	}

	return resp.ApplicationscimJSON200, nil
}

// patch partially updates an agent definition.
func patch(ctx context.Context, c resource.Conn, id string, v *registry.Agent) (*registry.Agent, error) {
	client, err := newClient(ctx, c)
	if err != nil {
		return nil, err
	}

	var ops []registry.PatchOperation

	if v.DisplayName != "" {
		var val registry.PatchOperation_Value
		if err := val.FromPatchOperationValue0(v.DisplayName); err != nil {
			return nil, err
		}
		ops = append(ops, registry.PatchOperation{
			Op:    registry.Replace,
			Path:  registry.PatchOperationPathDisplayName,
			Value: &val,
		})
	}

	if v.Description != nil {
		if *v.Description == "" {
			ops = append(ops, registry.PatchOperation{
				Op:   registry.Remove,
				Path: registry.PatchOperationPathDescription,
			})
		} else {
			var val registry.PatchOperation_Value
			if err := val.FromPatchOperationValue0(*v.Description); err != nil {
				return nil, err
			}
			ops = append(ops, registry.PatchOperation{
				Op:    registry.Replace,
				Path:  registry.PatchOperationPathDescription,
				Value: &val,
			})
		}
	}

	if v.Status != nil {
		var val registry.PatchOperation_Value
		if err := val.FromPatchOperationValue1(registry.PatchOperationValue1(*v.Status)); err != nil {
			return nil, err
		}
		ops = append(ops, registry.PatchOperation{
			Op:    registry.Replace,
			Path:  registry.PatchOperationPathStatus,
			Value: &val,
		})
	}

	if v.Tags != nil {
		var val registry.PatchOperation_Value
		if err := val.FromPatchOperationValue2(*v.Tags); err != nil {
			return nil, err
		}
		ops = append(ops, registry.PatchOperation{
			Op:    registry.Replace,
			Path:  registry.PatchOperationPathTags,
			Value: &val,
		})
	}

	if v.ExternalIds != nil {
		var val registry.PatchOperation_Value
		if err := val.FromPatchOperationValue2(*v.ExternalIds); err != nil {
			return nil, err
		}
		ops = append(ops, registry.PatchOperation{
			Op:    registry.Replace,
			Path:  registry.PatchOperationPathExternalIds,
			Value: &val,
		})
	}

	if v.Owners != nil {
		var val registry.PatchOperation_Value
		if err := val.FromPatchOperationValue3(*v.Owners); err != nil {
			return nil, err
		}
		ops = append(ops, registry.PatchOperation{
			Op:    registry.Replace,
			Path:  registry.PatchOperationPathOwners,
			Value: &val,
		})
	}

	if len(ops) == 0 {
		return get(ctx, c, id)
	}

	req := registry.PatchRequest{
		Operations: ops,
		Schemas:    []string{"urn:ietf:params:scim:api:messages:2.0:PatchOp"},
	}

	resp, err := client.PatchV10AgentsIdWithApplicationScimPlusJSONBodyWithResponse(ctx, id, req)
	if err != nil {
		return nil, err
	}

	if resp.HTTPResponse.StatusCode != http.StatusNoContent && resp.HTTPResponse.StatusCode != http.StatusOK {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON400 != nil {
			scimErr = resp.ApplicationscimJSON400
		} else if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON404 != nil {
			scimErr = resp.ApplicationscimJSON404
		} else if resp.ApplicationscimJSON409 != nil {
			scimErr = resp.ApplicationscimJSON409
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return nil, parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, id)
	}

	return get(ctx, c, id)
}

// remove deletes an agent by ID.
func remove(ctx context.Context, c resource.Conn, id string) error {
	client, err := newClient(ctx, c)
	if err != nil {
		return err
	}

	resp, err := client.DeleteV10AgentsIdWithResponse(ctx, id)
	if err != nil {
		return err
	}

	if resp.HTTPResponse.StatusCode != http.StatusNoContent {
		var scimErr *registry.SCIMError
		if resp.ApplicationscimJSON401 != nil {
			scimErr = resp.ApplicationscimJSON401
		} else if resp.ApplicationscimJSON403 != nil {
			scimErr = resp.ApplicationscimJSON403
		} else if resp.ApplicationscimJSON404 != nil {
			scimErr = resp.ApplicationscimJSON404
		} else if resp.ApplicationscimJSON409 != nil {
			scimErr = resp.ApplicationscimJSON409
		} else if resp.ApplicationscimJSON500 != nil {
			scimErr = resp.ApplicationscimJSON500
		} else if resp.ApplicationscimJSON503 != nil {
			scimErr = resp.ApplicationscimJSON503
		}
		return parseError(resp.HTTPResponse.StatusCode, resp.Body, scimErr, id)
	}

	return nil
}
