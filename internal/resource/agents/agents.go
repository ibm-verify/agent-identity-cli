package agents

import (
	"github.com/ibm-verify/agent-identity-cli/internal/client/registry"
	"github.com/ibm-verify/agent-identity-cli/internal/resource"
)

const template = `# Human-readable name of the agent. It is unique and required. Max 255 characters.
displayName: Production API Service
# What the agent is for. Max 1024 characters.
description: API service for production workloads
# Lifecycle status of the agent: ACTIVE, SUSPENDED or UNDER_REVIEW.
# The server uses UNDER_REVIEW when it is not given.
status: UNDER_REVIEW
# Identifiers of the agent in other systems. Each one is unique. Max 10 items.
# A patch replaces the whole list.
externalIds:
  - ext-agent-123
# Free-form labels for categorization and search. Max 10 tags.
# A patch replaces the whole list.
tags: [production, api-service]
# Users who own the agent. Max 5 owners. A patch replaces the whole list.
owners:
  # Only users are supported.
  - type: User
    # The user ID in the configured user registry.
    value: user-123
`

// Resource returns the definition of agents, which is all that cmd.NewRoot
// needs.
func Resource() *resource.Resource {
	d := &resource.Def[registry.Agent]{
		Name:     "agents",
		Singular: "agent",
		Aliases:  []string{"agent"},
		Template: template,
		Columns: []resource.Column[registry.Agent]{
			{Header: "STATUS", Value: func(a *registry.Agent) string { return resource.Str((*string)(a.Status)) }},
			{Header: "TAGS", Value: func(a *registry.Agent) string {
				if a.Tags == nil {
					return ""
				}
				return resource.Join(*a.Tags)
			}},
			{Header: "DESCRIPTION", Value: func(a *registry.Agent) string { return resource.Str(a.Description) }},
		},
		ListFlags: resource.ListSupport{Search: true},

		ID:     func(a *registry.Agent) string { return resource.Str(a.Id) },
		NameOf: func(a *registry.Agent) string { return a.DisplayName },

		// The server assigns these, so input that carries them (the output of
		// get) is accepted and they are dropped.
		Strip: func(a *registry.Agent) {
			a.Id, a.Meta, a.CreatedBy, a.OauthClients = nil, nil, nil, nil
		},

		Get:     get,
		List:    list,
		Create:  create,
		Replace: replace,
		Patch:   patch,
		Delete:  remove,
	}
	return d.Resource()
}
