// Package openapi embute a especificação OpenAPI da registry-api.
package openapi

import _ "embed"

// Spec é o conteúdo de openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
