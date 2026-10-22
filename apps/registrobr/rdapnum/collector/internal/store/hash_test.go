package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHashIgnoresNotices(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "autnum-61610.json"))
	if err != nil {
		t.Fatal(err)
	}
	h1, err := Hash(b)
	if err != nil {
		t.Fatal(err)
	}
	// Outra hora e outro IP no aviso: o mesmo hash.
	other := strings.Replace(string(b), "2026-10-21T17:36:01-03:00 - 192.0.2.1", "2026-10-22T08:00:00-03:00 - 198.51.100.7", 1)
	if other == string(b) {
		t.Fatal("a fixture não tem o aviso esperado")
	}
	if h2, _ := Hash([]byte(other)); h2 != h1 {
		t.Errorf("hash mudou com o aviso: %s %s", h1, h2)
	}
	// Uma mudança fora do aviso muda o hash.
	changed := strings.Replace(string(b), "ELEA DATA CENTERS", "ELEA DATA CENTERS S.A.", 1)
	if h3, _ := Hash([]byte(changed)); h3 == h1 {
		t.Error("hash igual com o dado mudado")
	}
	if len(h1) != 64 || strings.ToLower(h1) != h1 {
		t.Errorf("hash = %q", h1)
	}
}

func TestNormalizeKeyOrder(t *testing.T) {
	a, _ := Normalize([]byte(`{"b":1,"a":{"y":[1,2],"x":1.50},"notices":[1]}`))
	b, _ := Normalize([]byte(" {\"a\":{\"x\":1.50,\"y\":[1,2]},\"b\":1}\n"))
	if string(a) != string(b) || string(a) != `{"a":{"x":1.50,"y":[1,2]},"b":1}` {
		t.Errorf("normalizado = %s / %s", a, b)
	}
	// Só o notices de nível superior sai.
	n, _ := Normalize([]byte(`{"entities":[{"notices":["fica"]}]}`))
	if !strings.Contains(string(n), "fica") {
		t.Errorf("notices aninhado saiu: %s", n)
	}
	// 404: corpo vazio.
	e, err := Normalize(nil)
	if err != nil || len(e) != 0 {
		t.Errorf("vazio = %q %v", e, err)
	}
	if h, _ := Hash(nil); h != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("hash vazio = %s", h)
	}
	if _, err := Normalize([]byte(`{`)); err == nil {
		t.Error("JSON inválido normalizado")
	}
}
