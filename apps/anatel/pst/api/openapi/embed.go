// Package openapi embute o manifesto OpenAPI da api-anatel-pst.
package openapi

import _ "embed"

// Spec é o conteúdo de openapi.yaml.
//
//go:embed openapi.yaml
var Spec []byte
