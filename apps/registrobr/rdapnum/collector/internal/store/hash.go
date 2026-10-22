package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Normalize devolve o corpo normalizado de uma resposta para o hash
// (dados.md#resposta-igual): sem o notices de nível superior (muda a cada
// consulta: hora e IP de quem consultou) e com as chaves dos objetos em
// ordem (json.Marshal de um map). Os números ficam como vieram. Corpo vazio
// (o 404) normaliza para vazio.
func Normalize(body []byte) ([]byte, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("normalizando: %w", err)
	}
	if m, ok := v.(map[string]any); ok {
		delete(m, "notices")
	}
	return json.Marshal(v)
}

// Hash é o SHA-256, em hex minúsculo, do corpo normalizado.
func Hash(body []byte) (string, error) {
	n, err := Normalize(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(n)
	return hex.EncodeToString(sum[:]), nil
}
