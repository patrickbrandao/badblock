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

	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/dataset"
	"github.com/patrickbrandao/badblock/apps/asnames/api/internal/realip"
	"github.com/patrickbrandao/badblock/apps/asnames/api/openapi"
)

// Servidores do manifesto: a versão atual e a v1 fixa, em produção e local.
var (
	rootServers = []string{
		"https://api.badblock.net.br/asnames",
		"https://api.badblock.net.br/asnames/v1",
		"http://127.0.0.1:8108/asnames",
		"http://127.0.0.1:8108/asnames/v1",
	}
	// unversionedServers é o que as rotas fora do versionamento (saúde,
	// openapi.yaml) declaram no próprio path item.
	unversionedServers = []string{
		"https://api.badblock.net.br/asnames",
		"http://127.0.0.1:8108/asnames",
	}
)

// yamlRef é um $ref e o JSON pointer de onde ele aparece.
type yamlRef struct{ at, ref string }

// yamlIndex é o mínimo do manifesto que o teste precisa, lido linha a linha
// (sem dependência de YAML).
type yamlIndex struct {
	keys         map[string]bool     // JSON pointer de cada chave de mapa
	refs         []yamlRef           // todos os $ref, inclusive os de listas
	servers      map[string][]string // dono ("" = raiz, "/paths/~1ping"...) → URLs, na ordem
	operationIDs []string            // todos os operationId, na ordem do arquivo
}

// yamlKey casa "chave: valor" ou "chave:" (chave simples ou entre aspas).
var yamlKey = regexp.MustCompile(`^('[^']*'|"[^"]*"|[^\s#'"\-][^:]*?):(?:\s+(.*))?$`)

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' && s[len(s)-1] == '\'' || s[0] == '"' && s[len(s)-1] == '"') {
		return s[1 : len(s)-1]
	}
	return s
}

// parseYAML indexa o manifesto. Entende o subconjunto usado nele: mapas em
// bloco, listas com "- ", escalares numa linha só e blocos "|"/">".
func parseYAML(t *testing.T, doc []byte) yamlIndex {
	t.Helper()
	type frame struct {
		indent int
		name   string // chave já escapada como segmento de JSON pointer
	}
	idx := yamlIndex{keys: map[string]bool{}, servers: map[string][]string{}}
	var stack []frame
	pop := func(indent int) {
		for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
			stack = stack[:len(stack)-1]
		}
	}
	pointer := func(name string) string {
		var b strings.Builder
		for _, f := range stack {
			b.WriteString("/" + f.name)
		}
		return b.String() + "/" + name
	}
	block := -1 // indentação da chave dona de um bloco "|"/">" em andamento
	for n, line := range strings.Split(string(doc), "\n") {
		if strings.Contains(line, "\t") {
			t.Fatalf("linha %d: tab na indentação do YAML", n+1)
		}
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 {
			if trimmed == "" || indent > block {
				continue
			}
			block = -1
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rest := line[indent:]
		if rest == "-" || strings.HasPrefix(rest, "- ") {
			pop(indent)
			stack = append(stack, frame{indent, "-"})
			after := strings.TrimLeft(rest[1:], " ")
			indent += len(rest) - len(after)
			rest = after
		}
		m := yamlKey.FindStringSubmatch(rest)
		if m == nil {
			continue // item escalar de lista
		}
		key, val := unquote(m[1]), strings.TrimSpace(m[2])
		name := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
		pop(indent)
		p := pointer(name)
		idx.keys[p] = true
		switch {
		case key == "$ref":
			idx.refs = append(idx.refs, yamlRef{p, unquote(val)})
		case key == "operationId":
			idx.operationIDs = append(idx.operationIDs, unquote(val))
		case strings.HasSuffix(p, "/servers/-/url"):
			owner := strings.TrimSuffix(p, "/servers/-/url")
			idx.servers[owner] = append(idx.servers[owner], unquote(val))
		}
		if val != "" && (val[0] == '|' || val[0] == '>') {
			block = indent
		}
		stack = append(stack, frame{indent, name})
	}
	return idx
}

var httpMethods = []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}

// pathParam troca {nome} por {} (o nome do parâmetro não importa).
var pathParam = regexp.MustCompile(`\{[^}]*\}`)

// documentedRoutes devolve as rotas que o manifesto promete, no formato
// "MÉTODO caminho" sem o caminho de base: um path sem servers próprios vale
// nos servidores da raiz, isto é, em X e em /v1/X; um path com servers
// próprios (só os sem /v1) vale só em X.
func documentedRoutes(t *testing.T, idx yamlIndex) []string {
	t.Helper()
	var out []string
	for p := range idx.keys {
		seg := strings.Split(strings.TrimPrefix(p, "/"), "/")
		if len(seg) != 3 || seg[0] != "paths" {
			continue
		}
		path := strings.ReplaceAll(strings.ReplaceAll(seg[1], "~1", "/"), "~0", "~")
		path = pathParam.ReplaceAllString(path, "{}")
		switch {
		case seg[2] == "$ref":
			t.Errorf("paths %s: $ref no path item; cada rota fica direto em paths, uma vez só", path)
		case slices.Contains(httpMethods, seg[2]):
			m := strings.ToUpper(seg[2])
			if own, ok := idx.servers["/paths/"+seg[1]]; ok {
				if !slices.Equal(own, unversionedServers) {
					t.Errorf("paths %s: servers próprios = %v, quero %v", path, own, unversionedServers)
				}
				out = append(out, m+" "+path)
			} else {
				out = append(out, m+" "+path, m+" /v1"+path)
			}
		}
	}
	slices.Sort(out)
	return out
}

func TestOpenAPIManifest(t *testing.T) {
	if !bytes.HasPrefix(openapi.Spec, []byte("openapi: 3.1.")) {
		t.Fatalf("o manifesto deveria começar com \"openapi: 3.1.\": %.40q", openapi.Spec)
	}
	idx := parseYAML(t, openapi.Spec)
	if got := idx.servers[""]; !slices.Equal(got, rootServers) {
		t.Errorf("servers da raiz = %v, quero %v", got, rootServers)
	}

	// operationId único no arquivo.
	seen := map[string]bool{}
	for _, id := range idx.operationIDs {
		if seen[id] {
			t.Errorf("operationId repetido: %s", id)
		}
		seen[id] = true
	}
	if len(idx.operationIDs) == 0 {
		t.Error("nenhum operationId no manifesto")
	}

	// Todo $ref aponta para algo que existe no arquivo.
	for _, r := range idx.refs {
		if !strings.HasPrefix(r.ref, "#/") || !idx.keys[strings.TrimPrefix(r.ref, "#")] {
			t.Errorf("%s: $ref %q não existe no manifesto", r.at, r.ref)
		}
	}

	// Toda rota registrada está documentada, e vice-versa. Ficam de fora o
	// redirect sem barra e o catch-all "/".
	rip, err := realip.New(realip.DefaultTrustedProxies, realip.DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	a := New(&fakeStore{}, fixedView{dataset.Snapshot{Version: "0192-v1", AppliedAt: time.Unix(0, 0)}},
		&memCache{m: map[string][]byte{}}, rip, slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config{BasePath: "/asnames/", Version: "test", DBTimeout: time.Second})
	a.Handler()
	var registered []string
	for _, pattern := range a.routes {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok || path == a.cfg.BasePath {
			continue
		}
		path = strings.ReplaceAll(strings.TrimPrefix(path, a.cfg.BasePath), "{$}", "")
		registered = append(registered, method+" "+pathParam.ReplaceAllString(path, "{}"))
	}
	slices.Sort(registered)
	if len(registered) == 0 {
		t.Fatal("nenhuma rota registrada em a.routes")
	}
	documented := documentedRoutes(t, idx)
	for _, r := range registered {
		if !slices.Contains(documented, r) {
			t.Errorf("rota registrada fora do manifesto: %s", r)
		}
	}
	for _, d := range documented {
		if !slices.Contains(registered, d) {
			t.Errorf("rota no manifesto que o código não registra: %s", d)
		}
	}
}

func TestOpenAPIRoute(t *testing.T) {
	h, _ := newAPI(t, &fakeStore{}, false)
	for _, method := range []string{"GET", "HEAD"} {
		rec := do(h, method, "/asnames/openapi.yaml")
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/yaml" ||
			rec.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Errorf("%s /openapi.yaml = %d %v", method, rec.Code, rec.Header())
		}
		if method == "GET" && !bytes.Equal(rec.Body.Bytes(), openapi.Spec) {
			t.Errorf("corpo de /openapi.yaml diferente de openapi.Spec (%d bytes)", rec.Body.Len())
		}
	}
	if rec := do(h, "GET", "/asnames/v1/openapi.yaml"); rec.Code != 404 {
		t.Errorf("/openapi.yaml fica fora do versionamento: %d", rec.Code)
	}
	idx := decode[IndexResponse](t, do(h, "GET", "/asnames/"))
	if !slices.Contains(idx.Endpoints, "/asnames/openapi.yaml") {
		t.Errorf("índice sem /asnames/openapi.yaml: %v", idx.Endpoints)
	}
}
