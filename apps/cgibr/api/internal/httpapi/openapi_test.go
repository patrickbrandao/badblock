package httpapi

import (
	"bytes"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/cgibr/api/openapi"
)

// yamlEntry é uma chave do manifesto com o caminho completo até ela.
type yamlEntry struct {
	path []string
	val  string
}

// yamlKeys lê o YAML linha a linha (só biblioteca padrão) e devolve cada
// chave com o caminho de chaves até ela. Itens de lista ("- chave: valor")
// entram no caminho do pai, e o conteúdo de blocos "|"/">" é pulado. Basta
// para achar paths, métodos, $ref e servers do manifesto.
func yamlKeys(src string) []yamlEntry {
	type frame struct {
		indent int
		key    string
	}
	var (
		out   []yamlEntry
		stack []frame
		block = -1 // indentação da chave que abriu um bloco "|"/">"
	)
	for line := range strings.SplitSeq(src, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if block >= 0 {
			if trimmed == "" || indent > block {
				continue
			}
			block = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
		if rest, ok := strings.CutPrefix(trimmed, "- "); ok {
			indent += 2
			trimmed = rest
		}
		key, val, ok := splitYAMLKey(trimmed)
		if !ok {
			continue
		}
		path := make([]string, 0, len(stack)+1)
		for _, f := range stack {
			path = append(path, f.key)
		}
		path = append(path, key)
		out = append(out, yamlEntry{path: path, val: val})
		stack = append(stack, frame{indent: indent, key: key})
		if strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">") {
			block = indent
		}
	}
	return out
}

// splitYAMLKey separa "chave: valor" (chave e valor sem aspas).
func splitYAMLKey(s string) (key, val string, ok bool) {
	if s == "" {
		return "", "", false
	}
	if s[0] == '\'' || s[0] == '"' {
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 || len(s) < end+3 || s[end+2] != ':' {
			return "", "", false
		}
		key, s = s[1:end+1], s[end+2:]
		if s != ":" && !strings.HasPrefix(s, ": ") {
			return "", "", false
		}
		return key, unquoteYAML(strings.TrimSpace(s[1:])), true
	}
	if i := strings.Index(s, ": "); i > 0 {
		return s[:i], unquoteYAML(strings.TrimSpace(s[i+2:])), true
	}
	if strings.HasSuffix(s, ":") {
		return s[:len(s)-1], "", true
	}
	return "", "", false
}

func unquoteYAML(s string) string {
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

var (
	httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	pathParam   = regexp.MustCompile(`\{[^}]*\}`)
)

const (
	prodServer  = "https://api.badblock.net.br/cgibr"
	localServer = "http://127.0.0.1:8101/cgibr"
)

// openAPIOperations devolve "MÉTODO /caminho" de cada operação do manifesto
// (nomes de parâmetro viram {}), com true quando o path item vale também
// para a v1 fixa (não tem servers próprio) e false quando sobrescreve
// servers (rota fora do versionamento).
func openAPIOperations(t *testing.T, entries []yamlEntry) map[string]bool {
	t.Helper()
	ownServers := map[string][]string{}
	for _, e := range entries {
		if len(e.path) == 4 && e.path[0] == "paths" && e.path[2] == "servers" && e.path[3] == "url" {
			ownServers[e.path[1]] = append(ownServers[e.path[1]], e.val)
		}
	}
	for p, urls := range ownServers {
		if !slices.Equal(urls, []string{prodServer, localServer}) {
			t.Errorf("servers de %s = %v, quero só produção e local sem /v1", p, urls)
		}
	}
	ops := map[string]bool{}
	for _, e := range entries {
		if len(e.path) != 3 || e.path[0] != "paths" {
			continue
		}
		if e.path[2] == "$ref" {
			t.Errorf("%s usa $ref; cada rota deve estar direto em paths", e.path[1])
		}
		if slices.Contains(httpMethods, e.path[2]) {
			_, own := ownServers[e.path[1]]
			ops[strings.ToUpper(e.path[2])+" "+pathParam.ReplaceAllString(e.path[1], "{}")] = !own
		}
	}
	return ops
}

// registeredOperations devolve "MÉTODO /caminho" das rotas registradas, sem o
// caminho de base e sem o /v1, com true quando a rota também existe na v1
// fixa. Ficam de fora o redirect sem barra e o catch-all "/".
func registeredOperations(t *testing.T, routes []string, base string) map[string]bool {
	t.Helper()
	raw := map[string]bool{}
	for _, r := range routes {
		method, path, ok := strings.Cut(r, " ")
		if !ok {
			continue // catch-all "/", sem método
		}
		rest, ok := strings.CutPrefix(path, base)
		if !ok {
			t.Errorf("rota fora do caminho de base: %s", r)
			continue
		}
		if rest == "" {
			continue // redirect de {base} para {base}/
		}
		rest = strings.Replace(rest, "{$}", "", 1)
		raw[method+" "+pathParam.ReplaceAllString(rest, "{}")] = true
	}
	v1 := "/" + CurrentAPIVersion
	ops := map[string]bool{}
	for op := range raw {
		method, path, _ := strings.Cut(op, " ")
		if rest, ok := strings.CutPrefix(path, v1+"/"); ok {
			ops[method+" /"+rest] = true
			continue
		}
		if raw[method+" "+v1+path] {
			ops[op] = true
		} else if _, seen := ops[op]; !seen {
			ops[op] = false
		}
	}
	return ops
}

func newTestAPI(t *testing.T) *API {
	t.Helper()
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	snap := dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0)}
	return New(&fakeStore{}, fixedView{snap}, &memCache{m: map[string][]byte{}}, rip,
		slog.New(slog.NewTextHandler(io.Discard, nil)), Config{BasePath: "/cgibr/", Version: "test"})
}

func TestOpenAPIHeaderAndServers(t *testing.T) {
	src := string(openapi.Spec)
	for line := range strings.SplitSeq(src, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "openapi: 3.1.") {
			t.Errorf("primeira linha = %q, quero openapi: 3.1.x", line)
		}
		break
	}
	var servers []string
	for _, e := range yamlKeys(src) {
		if slices.Equal(e.path, []string{"servers", "url"}) {
			servers = append(servers, e.val)
		}
	}
	want := []string{prodServer, prodServer + "/v1", localServer, localServer + "/v1"}
	if !slices.Equal(servers, want) {
		t.Errorf("servers = %v, quero %v", servers, want)
	}
}

func TestOpenAPICoversRoutes(t *testing.T) {
	a := newTestAPI(t)
	a.Handler()
	registered := registeredOperations(t, a.routes, a.cfg.BasePath)
	documented := openAPIOperations(t, yamlKeys(string(openapi.Spec)))
	if len(registered) == 0 || len(documented) == 0 {
		t.Fatalf("registradas = %d, documentadas = %d", len(registered), len(documented))
	}
	for op, inV1 := range registered {
		doc, ok := documented[op]
		switch {
		case !ok:
			t.Errorf("rota registrada fora do openapi.yaml: %s", op)
		case inV1 && !doc:
			t.Errorf("%s existe na v1, mas o path tem servers próprio (sem /v1)", op)
		case !inV1 && doc:
			t.Errorf("%s não existe na v1; o path precisa de servers próprio sem /v1", op)
		}
	}
	for op := range documented {
		if _, ok := registered[op]; !ok {
			t.Errorf("openapi.yaml descreve rota que não existe: %s", op)
		}
	}
}

func TestOpenAPIOperationIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range yamlKeys(string(openapi.Spec)) {
		if e.path[len(e.path)-1] != "operationId" {
			continue
		}
		if seen[e.val] {
			t.Errorf("operationId repetido: %s", e.val)
		}
		seen[e.val] = true
	}
	if len(seen) == 0 {
		t.Error("nenhum operationId encontrado")
	}
}

func TestOpenAPIRefsResolve(t *testing.T) {
	entries := yamlKeys(string(openapi.Spec))
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
		ptr, ok := strings.CutPrefix(e.val, "#/")
		if !ok {
			t.Errorf("$ref externo ou inválido: %q", e.val)
			continue
		}
		toks := strings.Split(ptr, "/")
		for i, tok := range toks {
			toks[i] = strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~")
		}
		if !keys[strings.Join(toks, "\x00")] {
			t.Errorf("$ref aponta para o que não existe: %s", e.val)
		}
	}
	if refs == 0 {
		t.Error("nenhum $ref encontrado; o leitor de YAML quebrou?")
	}
}

func TestOpenAPIRoute(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, true)
	rec := do(h, "GET", "/cgibr/openapi.yaml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("openapi.yaml = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !bytes.Equal(rec.Body.Bytes(), openapi.Spec) {
		t.Error("corpo diferente de openapi.Spec")
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=300" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if rec := do(h, "GET", "/cgibr/v1/openapi.yaml"); rec.Code != 404 {
		t.Errorf("/v1/openapi.yaml = %d, quero 404 (fora do versionamento)", rec.Code)
	}
	if rec := do(h, "GET", "/cgibr/"); !strings.Contains(rec.Body.String(), `"/cgibr/openapi.yaml"`) {
		t.Errorf("índice sem o manifesto: %s", rec.Body)
	}
}
