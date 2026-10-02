package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/iana/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/iana/api/openapi"
)

// yamlEntry é uma chave de mapa do manifesto, com o caminho desde a raiz
// (ex.: components, schemas, Error) e o valor escrito na mesma linha.
type yamlEntry struct {
	path  []string
	value string
}

// parseYAML lê o YAML linha a linha (só o necessário para o manifesto, sem
// dependência nova): indentação por espaços, chaves "chave: valor", itens de
// lista "- chave: valor" e escalares de bloco (| e >), cujo texto é pulado.
func parseYAML(t *testing.T, src string) []yamlEntry {
	t.Helper()
	type level struct {
		indent int
		key    string
	}
	var (
		stack []level
		out   []yamlEntry
		block = -1 // indentação da chave que abriu um escalar de bloco
	)
	for n, raw := range strings.Split(src, "\n") {
		line := strings.TrimRight(raw, " \r")
		text := strings.TrimLeft(line, " ")
		indent := len(line) - len(text)
		if text == "" {
			continue
		}
		if strings.HasPrefix(raw, "\t") {
			t.Fatalf("linha %d: tabulação na indentação", n+1)
		}
		if block >= 0 {
			if indent > block {
				continue
			}
			block = -1
		}
		if strings.HasPrefix(text, "#") {
			continue
		}
		if text == "-" || strings.HasPrefix(text, "- ") {
			// Item de lista: a chave, se houver, fica depois do "- ".
			text = strings.TrimLeft(text[1:], " ")
			indent = len(line) - len(text)
		}
		key, value, ok := splitYAMLKey(text)
		if !ok {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		stack = append(stack, level{indent, key})
		path := make([]string, len(stack))
		for i, l := range stack {
			path[i] = l.key
		}
		if strings.HasPrefix(value, "|") || strings.HasPrefix(value, ">") {
			block = indent
			value = ""
		}
		out = append(out, yamlEntry{path: path, value: value})
	}
	return out
}

// splitYAMLKey separa "chave: valor" (a chave pode vir entre aspas). Linhas
// que não são chave de mapa (texto de lista, por exemplo) devolvem ok false.
func splitYAMLKey(text string) (key, value string, ok bool) {
	rest := ""
	switch text[0] {
	case '\'', '"':
		end := strings.IndexByte(text[1:], text[0])
		if end < 0 {
			return "", "", false
		}
		key, rest = text[1:end+1], text[end+2:]
		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		rest = rest[1:]
		if rest != "" && rest[0] != ' ' {
			return "", "", false
		}
	case '[', '{':
		return "", "", false
	default:
		if i := strings.Index(text, ": "); i >= 0 {
			key, rest = text[:i], text[i+1:]
		} else if strings.HasSuffix(text, ":") {
			key = text[:len(text)-1]
		} else {
			return "", "", false
		}
	}
	return key, unquoteYAML(strings.TrimSpace(rest)), true
}

func unquoteYAML(v string) string {
	if len(v) >= 2 && (v[0] == '\'' || v[0] == '"') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true,
}

// paramName troca o nome dos parâmetros de caminho por {} para comparar
// rotas do mux ({asn}) com as do manifesto ({asn}) sem depender do nome.
var paramName = regexp.MustCompile(`\{[^}]*\}`)

// specOperations devolve cada operação de paths ("MÉTODO /caminho") e se o
// path item dela sobrescreve servers (rotas fora do versionamento).
func specOperations(t *testing.T, entries []yamlEntry) map[string]bool {
	t.Helper()
	ownServers := map[string]bool{}
	for _, e := range entries {
		if len(e.path) == 3 && e.path[0] == "paths" && e.path[2] == "servers" {
			ownServers[e.path[1]] = true
		}
	}
	ops := map[string]bool{}
	for _, e := range entries {
		if len(e.path) != 3 || e.path[0] != "paths" {
			continue
		}
		switch {
		case httpMethods[e.path[2]]:
			ops[strings.ToUpper(e.path[2])+" "+paramName.ReplaceAllString(e.path[1], "{}")] = ownServers[e.path[1]]
		case e.path[2] == "$ref":
			t.Errorf("paths %s: $ref em path item; cada rota fica direto em paths", e.path[1])
		}
	}
	return ops
}

// registeredOperations devolve as rotas registradas no mux ("MÉTODO
// /caminho", sem o caminho de base): as da v1 fixa (/v1/X, devolvidas como
// X) e as sem versão. Ficam de fora o redirect sem barra e o catch-all "/",
// que não entram no manifesto.
func registeredOperations(t *testing.T, a *API) (versioned, unversioned map[string]bool) {
	t.Helper()
	versioned, unversioned = map[string]bool{}, map[string]bool{}
	for _, pattern := range a.routes {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			continue // catch-all sem método
		}
		rel, ok := strings.CutPrefix(path, a.cfg.BasePath)
		if !ok {
			t.Errorf("rota %q fora do caminho de base %s", pattern, a.cfg.BasePath)
			continue
		}
		if rel == "" {
			continue // redirect /iana → /iana/
		}
		target := unversioned
		if x, ok := strings.CutPrefix(rel, "/"+CurrentAPIVersion+"/"); ok {
			rel, target = "/"+x, versioned
		}
		rel = strings.TrimSuffix(rel, "{$}")
		target[method+" "+paramName.ReplaceAllString(rel, "{}")] = true
	}
	return versioned, unversioned
}

func newTestAPI(t *testing.T) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	return New(&fakeStore{}, fixedView{}, &memCache{m: map[string][]byte{}}, rip,
		slog.New(slog.NewTextHandler(io.Discard, nil)), Config{BasePath: "/iana", Version: "test"})
}

func TestOpenAPIManifest(t *testing.T) {
	src := string(openapi.Spec)
	if !strings.HasPrefix(src, "openapi: 3.1.") {
		t.Fatalf("o manifesto deve começar com \"openapi: 3.1.\": %.40q", src)
	}
	entries := parseYAML(t, src)

	// servers da raiz: versão atual e v1 fixa, em produção e local.
	var servers []string
	pathServers := map[string][]string{}
	for _, e := range entries {
		switch {
		case slices.Equal(e.path, []string{"servers", "url"}):
			servers = append(servers, e.value)
		case len(e.path) == 4 && e.path[0] == "paths" && e.path[2] == "servers" && e.path[3] == "url":
			pathServers[e.path[1]] = append(pathServers[e.path[1]], e.value)
		}
	}
	prod, local := "https://api.badblock.net.br/iana", "http://127.0.0.1:8107/iana"
	v := "/" + CurrentAPIVersion
	if want := []string{prod, prod + v, local, local + v}; !slices.Equal(servers, want) {
		t.Errorf("servers = %v, quer %v", servers, want)
	}
	for p, list := range pathServers {
		if want := []string{prod, local}; !slices.Equal(list, want) {
			t.Errorf("paths %s: servers = %v, quer %v (só os sem %s)", p, list, want, v)
		}
	}

	// Toda rota registrada está no manifesto com o método, e vice-versa:
	// /v1/X ↔ path X sem servers próprio; rota só sem versão (saúde,
	// openapi.yaml) ↔ path com servers próprio.
	a := newTestAPI(t)
	a.Handler()
	ops := specOperations(t, entries)
	versioned, unversioned := registeredOperations(t, a)
	if len(versioned) == 0 || len(unversioned) == 0 {
		t.Fatalf("rotas registradas: v1 %v, sem versão %v", versioned, unversioned)
	}
	for op := range versioned {
		if own, ok := ops[op]; !ok || own {
			method, path, _ := strings.Cut(op, " ")
			t.Errorf("rota %s %s%s: o manifesto precisa do path %s sem servers próprio (existe=%v, servers próprio=%v)",
				method, v, path, path, ok, own)
		}
	}
	for op := range unversioned {
		if versioned[op] {
			continue
		}
		if own, ok := ops[op]; !ok || !own {
			t.Errorf("rota %s (sem %s): o manifesto precisa do path com servers próprio (existe=%v, servers próprio=%v)", op, v, ok, own)
		}
	}
	for op, own := range ops {
		switch {
		case !own && (!versioned[op] || !unversioned[op]):
			t.Errorf("manifesto descreve %s para os servidores com e sem %s, mas a rota não existe nas duas formas", op, v)
		case own && (!unversioned[op] || versioned[op]):
			t.Errorf("manifesto descreve %s só sem %s, mas o registro não bate (sem versão=%v, %s=%v)", op, v, unversioned[op], v, versioned[op])
		}
	}

	// operationId não se repete.
	seen := map[string]bool{}
	for _, e := range entries {
		if n := len(e.path); n >= 3 && e.path[n-1] == "operationId" && httpMethods[e.path[n-2]] {
			if seen[e.value] {
				t.Errorf("operationId repetido: %s", e.value)
			}
			seen[e.value] = true
		}
	}
	if len(seen) != len(ops) {
		t.Errorf("%d operationIds para %d operações", len(seen), len(ops))
	}

	// Todo $ref local aponta para algo que existe no arquivo.
	keys := map[string]bool{}
	for _, e := range entries {
		keys[strings.Join(e.path, "\x00")] = true
	}
	refs := 0
	for _, e := range entries {
		if e.path[len(e.path)-1] != "$ref" {
			continue
		}
		refs++
		ptr, ok := strings.CutPrefix(e.value, "#/")
		if !ok {
			t.Errorf("%s: $ref externo %q", strings.Join(e.path, "/"), e.value)
			continue
		}
		parts := strings.Split(ptr, "/")
		for i, p := range parts {
			parts[i] = strings.NewReplacer("~1", "/", "~0", "~").Replace(p)
		}
		if !keys[strings.Join(parts, "\x00")] {
			t.Errorf("%s: $ref %q não existe", strings.Join(e.path, "/"), e.value)
		}
	}
	if refs == 0 {
		t.Error("nenhum $ref encontrado: o parser do teste não leu o manifesto")
	}
}

func TestOpenAPIRoute(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, false) // não depende dos dados
	for _, method := range []string{"GET", "HEAD"} {
		rec := do(h, method, "/iana/openapi.yaml")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/yaml" ||
			rec.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Fatalf("%s /iana/openapi.yaml = %d %v", method, rec.Code, rec.Header())
		}
		if method == "GET" && !bytes.Equal(rec.Body.Bytes(), openapi.Spec) {
			t.Error("corpo de /iana/openapi.yaml difere de openapi.Spec")
		}
	}
	if rec := do(h, "GET", "/iana/v1/openapi.yaml"); rec.Code != 404 {
		t.Errorf("/iana/v1/openapi.yaml = %d, quer 404 (fora do versionamento)", rec.Code)
	}

	idx := decode[IndexResponse](t, do(h, "GET", "/iana/"))
	if !slices.Contains(idx.Endpoints, "/iana/openapi.yaml") {
		t.Errorf("índice sem /iana/openapi.yaml: %v", idx.Endpoints)
	}
}
