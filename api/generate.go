// Package api holds the OpenAPI specifications and code generation settings
// for each resource. Run `make generate` to regenerate the clients.
package api

//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config registry.codegen.yaml openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config oauth2.codegen.yaml openapi.yaml
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 -config platform.codegen.yaml openapi.yaml
