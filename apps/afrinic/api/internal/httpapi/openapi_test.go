package httpapi

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/patrickbrandao/badblock/apps/afrinic/api/internal/cache"
	"github.com/patrickbrandao/badblock/apps/afrinic/api/internal/rir"
	"github.com/patrickbrandao/badblock/apps/afrinic/api/openapi"
)

// O manifesto é lido linha a linha, sem biblioteca de YAML: basta para o
// subconjunto que ele usa (mapas em bloco, itens de lista "- ", listas em
// fluxo numa linha só e escalares em bloco | e >).

// yamlKey é uma linha "chave: valor" do manifesto, com o caminho de chaves
// até ela (ex.: [paths /asn/{asn} get operationId]).
type yamlKey struct {
	path  []string
	value string
}

var (
	blockScalar = regexp.MustCompile(`^[|>][-+0-9]*$`)
	refPattern  = regexp.MustCompile(`\$ref:\s*['"]?([^'"\s,}]+)`)
	pathParam   = regexp.MustCompile(`\{[^}]*\}`)
	httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
)

// yamlKeys devolve as chaves do manifesto na ordem do arquivo. Comentários,
// linhas em branco, itens de lista sem chave e o conteúdo dos escalares em
// bloco são pulados; num item "- chave: valor", a chave conta na coluna
// depois do "- ".
func yamlKeys(t *testing.T, spec []byte) []yamlKey {
	t.Helper()
	type level struct {
		indent int
		key    string
	}
	var (
		out     []yamlKey
		stack   []level
		blockAt = -1 // indentação da chave de um escalar em bloco aberto
	)
	sc := bufio.NewScanner(bytes.NewReader(spec))
	for n := 1; sc.Scan(); n++ {
		raw := strings.TrimRight(sc.Text(), " ")
		text := strings.TrimLeft(raw, " ")
		indent := len(raw) - len(text)
		if blockAt >= 0 {
			if text == "" || indent > blockAt {
				continue
			}
			blockAt = -1
		}
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		if strings.HasPrefix(text, "\t") {
			t.Fatalf("openapi.yaml:%d: tabulação na indentação", n)
		}
		if item, ok := strings.CutPrefix(text, "- "); ok {
			indent += 2
			text = item
		}
		key, value, ok := splitKey(text)
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
		out = append(out, yamlKey{path: path, value: value})
		if blockScalar.MatchString(value) {
			blockAt = indent
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// splitKey separa "chave: valor" ou "chave:", com a chave entre aspas ou não.
func splitKey(text string) (key, value string, ok bool) {
	if q := text[0]; q == '\'' || q == '"' {
		end := strings.IndexByte(text[1:], q)
		if end < 0 {
			return "", "", false
		}
		key, rest := text[1:1+end], text[2+end:]
		if rest == ":" {
			return key, "", true
		}
		if v, ok := strings.CutPrefix(rest, ": "); ok {
			return key, strings.TrimSpace(v), true
		}
		return "", "", false
	}
	if k, ok := strings.CutSuffix(text, ":"); ok && !strings.Contains(k, ": ") {
		return k, "", true
	}
	k, v, ok := strings.Cut(text, ": ")
	return k, strings.TrimSpace(v), ok
}

func unquote(s string) string { return strings.Trim(s, `'"`) }

// normPath ignora os nomes dos parâmetros: /holder/{id} = /holder/{opaque_id}.
func normPath(p string) string { return pathParam.ReplaceAllString(p, "{}") }

// pathDoc é uma entrada de paths do manifesto.
type pathDoc struct {
	methods []string // em maiúsculas
	servers []string // servers próprios (vazio = os da raiz, com a v1 fixa)
}

// manifest é o que os testes conferem no openapi.yaml.
type manifest struct {
	title   string
	servers []string            // servidores da raiz
	paths   map[string]*pathDoc // chave: caminho normalizado (normPath)
	opIDs   map[string][]string // operationId → operações que o usam
	ops     int                 // operações em paths
	defined map[string]bool     // "#/components/<tipo>/<nome>"
}

func parseManifest(t *testing.T, spec []byte) manifest {
	t.Helper()
	m := manifest{paths: map[string]*pathDoc{}, opIDs: map[string][]string{}, defined: map[string]bool{}}
	for _, k := range yamlKeys(t, spec) {
		p := k.path
		switch {
		case len(p) == 2 && p[0] == "info" && p[1] == "title":
			m.title = unquote(k.value)
		case len(p) == 2 && p[0] == "servers" && p[1] == "url":
			m.servers = append(m.servers, unquote(k.value))
		case len(p) == 3 && p[0] == "components":
			m.defined["#/components/"+p[1]+"/"+p[2]] = true
		case len(p) >= 2 && p[0] == "paths":
			d := m.paths[normPath(p[1])]
			if d == nil {
				d = &pathDoc{}
				m.paths[normPath(p[1])] = d
			}
			switch {
			case len(p) == 3 && slices.Contains(httpMethods, p[2]):
				d.methods = append(d.methods, strings.ToUpper(p[2]))
				m.ops++
			case len(p) == 4 && p[2] == "servers" && p[3] == "url":
				d.servers = append(d.servers, unquote(k.value))
			case len(p) == 4 && slices.Contains(httpMethods, p[2]) && p[3] == "operationId":
				id := unquote(k.value)
				m.opIDs[id] = append(m.opIDs[id], strings.ToUpper(p[2])+" "+p[1])
			}
		}
	}
	if len(m.paths) == 0 {
		t.Fatal("manifesto sem paths")
	}
	return m
}

// makefilePort é a porta do loopback do Makefile (PORT := ${..._HOST_PORT:-NNNN}).
func makefilePort(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^PORT\s*:=.*:-([0-9]+)\}`).FindSubmatch(b)
	if m == nil {
		t.Fatal("PORT não encontrado no Makefile")
	}
	return string(m[1])
}

// Toda rota registrada no servidor está no manifesto, e vice-versa. A v1 fixa
// é um servidor: o path X sem servers próprio vale em {base}X e em
// {base}/v1X; o path com servers próprio (saúde, manifesto) só em {base}X.
// Ficam de fora só o redirect de {base} para {base}/ e o 404 genérico.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	a := newTestAPI(t, &fakeStore{}, true, cache.Noop{})
	a.Handler()
	registered := map[string]bool{}
	for _, pattern := range a.routes {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			continue // "/": o 404 genérico não tem método
		}
		rest, ok := strings.CutPrefix(path, base)
		if !ok {
			t.Errorf("rota fora do caminho de base: %s", pattern)
			continue
		}
		if rest == "" {
			continue // redirect sem barra
		}
		registered[method+" "+normPath(strings.ReplaceAll(rest, "{$}", ""))] = true
	}
	if len(registered) == 0 {
		t.Fatal("nenhuma rota registrada")
	}

	v1 := "/" + CurrentAPIVersion
	documented := map[string]string{} // operação → de onde ela sai no manifesto
	for path, d := range parseManifest(t, openapi.Spec).paths {
		for _, m := range d.methods {
			documented[m+" "+path] = "paths " + path
			if len(d.servers) == 0 {
				documented[m+" "+v1+path] = "paths " + path + " (sem servers próprio, vale também na v1)"
			}
		}
	}
	for op := range registered {
		if _, ok := documented[op]; !ok {
			t.Errorf("rota registrada que falta no openapi.yaml: %s (rota só sem versão = path com servers próprio, sem /v1)", op)
		}
	}
	for op, from := range documented {
		if !registered[op] {
			t.Errorf("o openapi.yaml descreve %s (%s), que o servidor não registra", op, from)
		}
	}
}

// Versão 3.1, título, os quatro servidores da raiz (a porta local é a do
// Makefile), servers próprios só sem /v1, operationId único e todo $ref
// apontando para um componente que existe.
func TestOpenAPIDocument(t *testing.T) {
	spec := openapi.Spec
	if !bytes.HasPrefix(spec, []byte("openapi: 3.1.")) {
		t.Fatalf("o manifesto deveria começar com \"openapi: 3.1.\": %.40q", spec)
	}
	m := parseManifest(t, spec)
	if m.title != rir.App {
		t.Errorf("info.title = %q, quero %q", m.title, rir.App)
	}

	prod, local := "https://api.badblock.net.br"+base, "http://127.0.0.1:"+makefilePort(t)+base
	v1 := "/" + CurrentAPIVersion
	if want := []string{prod, prod + v1, local, local + v1}; !slices.Equal(m.servers, want) {
		t.Errorf("servers da raiz = %v, quero %v", m.servers, want)
	}
	for path, d := range m.paths {
		if len(d.servers) > 0 && !slices.Equal(d.servers, []string{prod, local}) {
			t.Errorf("paths %s: servers = %v, quero %v", path, d.servers, []string{prod, local})
		}
	}

	n := 0
	for id, ops := range m.opIDs {
		n += len(ops)
		if len(ops) > 1 {
			t.Errorf("operationId %s repetido em %v", id, ops)
		}
	}
	if n != m.ops {
		t.Errorf("%d operações e %d operationId: toda operação precisa de um", m.ops, n)
	}

	refs := refPattern.FindAllStringSubmatch(string(spec), -1)
	if len(refs) == 0 {
		t.Fatal("nenhum $ref no manifesto")
	}
	for _, r := range refs {
		if !m.defined[r[1]] {
			t.Errorf("$ref para um componente que não existe: %s", r[1])
		}
	}
}

// A rota serve o manifesto embutido, fora do versionamento, e o índice a lista
// (junto com rotas que estão todas no manifesto).
func TestOpenAPIRoute(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, false) // não depende dos dados
	rec := do(h, "GET", base+"/openapi.yaml")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/yaml" ||
		rec.Header().Get("Cache-Control") != "public, max-age=300" || !bytes.Equal(rec.Body.Bytes(), openapi.Spec) {
		t.Errorf("openapi.yaml = %d %v (%d bytes)", rec.Code, rec.Header(), rec.Body.Len())
	}
	if rec := do(h, "GET", base+"/v1/openapi.yaml"); rec.Code != 404 {
		t.Errorf("openapi.yaml é fora do versionamento: /v1 = %d", rec.Code)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Head(srv.URL + base + "/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || len(body) != 0 || resp.Header.Get("Content-Type") != "application/yaml" {
		t.Errorf("HEAD openapi.yaml = %d %q %v", resp.StatusCode, body, resp.Header)
	}

	idx := decode[IndexResponse](t, do(h, "GET", base+"/"))
	if !slices.Contains(idx.Endpoints, base+"/openapi.yaml") {
		t.Errorf("o índice deveria listar %s/openapi.yaml: %v", base, idx.Endpoints)
	}
	paths := parseManifest(t, openapi.Spec).paths
	for _, e := range idx.Endpoints {
		if d := paths[normPath(strings.TrimPrefix(e, base))]; d == nil || !slices.Contains(d.methods, "GET") {
			t.Errorf("o índice lista %s, que falta no openapi.yaml", e)
		}
	}
}
