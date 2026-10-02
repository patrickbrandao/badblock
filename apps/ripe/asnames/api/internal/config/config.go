// Package config lê a configuração da api-ripe-asnames. Cada opção assume, em ordem:
// o valor padrão, a variável de ambiente e o argumento de linha de comando.
package config

import (
	"flag"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrickbrandao/badblock/apps/ripe/asnames/api/internal/realip"
)

// Config é a configuração completa.
type Config struct {
	PostgresURL    string
	RedisURL       string
	CacheEnabled   bool
	CacheTTL       time.Duration
	CacheTimeout   time.Duration
	HTTPPort       int
	BasePath       string
	TrustedProxies string
	RealIPHeaders  string
	DBPoolMax      int
	DBTimeout      time.Duration
	DatasetPoll    time.Duration
	CORSOrigin     string
	AccessLog      bool
	LogLevel       string
	LogFormat      string
	ShowVersion    bool
}

type option struct {
	flag, env, def, usage string
}

var options = []option{
	{"postgres-url", "POSTGRES_URL", "", "URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)"},
	{"redis-url", "REDIS_URL", "", "URL do Valkey/Redis, ex.: redis://:senha@badblock-valkey:6379/0 (vazio desliga o cache)"},
	{"redis-cache-enabled", "REDIS_CACHE_ENABLED", "true", "liga ou desliga o cache"},
	{"redis-key-ttl", "REDIS_KEY_TTL", "3600", "validade das chaves de cache, em segundos"},
	{"redis-timeout", "REDIS_TIMEOUT", "50ms", "tempo máximo de cada operação no cache (fail-open)"},
	{"http-port", "HTTP_PORT", "8080", "porta HTTP"},
	{"base-path", "BASE_PATH", "/ripe/asnames", "caminho de base: a API responde tudo abaixo dele"},
	{"trusted-proxies", "TRUSTED_PROXIES", realip.DefaultTrustedProxies, "faixas cujos X-Forwarded-For/X-Real-IP são aceitos"},
	{"real-ip-headers", "REAL_IP_HEADERS", realip.DefaultHeaders, "ordem de leitura dos headers de IP real"},
	{"db-pool-max", "DB_POOL_MAX", "10", "conexões máximas no pool do Postgres"},
	{"db-timeout", "DB_TIMEOUT", "5s", "tempo máximo de cada consulta"},
	{"dataset-poll", "DATASET_POLL", "30s", "intervalo de conferência da versão dos dados"},
	{"cors-origin", "CORS_ORIGIN", "*", "valor de Access-Control-Allow-Origin"},
	{"access-log", "ACCESS_LOG", "true", "registra cada requisição no log"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("api-ripe-asnames", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { PrintHelp(stderr) }
	values := map[string]*string{}
	for _, o := range options {
		def := o.def
		if v := getenv(o.env); v != "" {
			def = v
		}
		values[o.flag] = fs.String(o.flag, def, o.usage)
	}
	c := &Config{}
	fs.BoolVar(&c.ShowVersion, "version", false, "mostra a versão e sai")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumento inesperado: %s", fs.Arg(0))
	}

	var err error
	c.PostgresURL = *values["postgres-url"]
	c.RedisURL = *values["redis-url"]
	c.TrustedProxies = *values["trusted-proxies"]
	c.RealIPHeaders = *values["real-ip-headers"]
	c.CORSOrigin = *values["cors-origin"]
	c.LogLevel = strings.ToLower(*values["log-level"])
	c.LogFormat = strings.ToLower(*values["log-format"])

	c.BasePath = "/" + strings.Trim(*values["base-path"], "/")
	if c.BasePath == "/" || strings.ContainsAny(c.BasePath, " {}?#") {
		return nil, fmt.Errorf("--base-path inválido: %q (ex.: /ripe/asnames)", *values["base-path"])
	}
	if c.CacheEnabled, err = strconv.ParseBool(*values["redis-cache-enabled"]); err != nil {
		return nil, fmt.Errorf("--redis-cache-enabled inválido: %q", *values["redis-cache-enabled"])
	}
	if c.AccessLog, err = strconv.ParseBool(*values["access-log"]); err != nil {
		return nil, fmt.Errorf("--access-log inválido: %q", *values["access-log"])
	}
	ttl, err := strconv.Atoi(*values["redis-key-ttl"])
	if err != nil || ttl <= 0 {
		return nil, fmt.Errorf("--redis-key-ttl inválido: %q", *values["redis-key-ttl"])
	}
	c.CacheTTL = time.Duration(ttl) * time.Second
	for _, d := range []struct {
		flag string
		dst  *time.Duration
	}{{"redis-timeout", &c.CacheTimeout}, {"db-timeout", &c.DBTimeout}, {"dataset-poll", &c.DatasetPoll}} {
		if *d.dst, err = time.ParseDuration(*values[d.flag]); err != nil || *d.dst <= 0 {
			return nil, fmt.Errorf("--%s inválido: %q", d.flag, *values[d.flag])
		}
	}
	if c.HTTPPort, err = strconv.Atoi(*values["http-port"]); err != nil || c.HTTPPort < 1 || c.HTTPPort > 65535 {
		return nil, fmt.Errorf("--http-port inválido: %q", *values["http-port"])
	}
	if c.DBPoolMax, err = strconv.Atoi(*values["db-pool-max"]); err != nil || c.DBPoolMax < 1 {
		return nil, fmt.Errorf("--db-pool-max inválido: %q", *values["db-pool-max"])
	}
	if _, err := realip.New(c.TrustedProxies, c.RealIPHeaders); err != nil {
		return nil, err
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("--log-level inválido: %q", c.LogLevel)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return nil, fmt.Errorf("--log-format inválido: %q", c.LogFormat)
	}
	if !c.ShowVersion && c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

// PrintHelp escreve o texto de ajuda.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `api-ripe-asnames — API HTTP dos nomes e países de todos os ASNs alocados (asn.txt do
RIPE NCC).

Lê as tabelas ripe_asnames_* mantidas pelo collector-ripe-asnames, com cache opcional no
Valkey. Responde tudo abaixo de BASE_PATH (/ripe/asnames): /ripe/asnames/asn/15169 é a
versão atual e /ripe/asnames/v1/asn/15169 fixa a v1.

Uso:
  api-ripe-asnames [opções]
  api-ripe-asnames healthcheck    (usa HTTP_PORT e BASE_PATH; para o HEALTHCHECK do Docker)

Opções (padrão → variável de ambiente → argumento):
`)
	sorted := append([]option(nil), options...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].flag < sorted[j].flag })
	for _, o := range sorted {
		def := o.def
		if def == "" {
			def = `""`
		}
		fmt.Fprintf(w, "  --%-20s %s\n  %22s env %s, padrão %s\n", o.flag, o.usage, "", o.env, def)
	}
	fmt.Fprint(w, `  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
`)
}

// Redact esconde a senha de uma URL para o log.
func Redact(u string) string {
	at := strings.LastIndex(u, "@")
	scheme := strings.Index(u, "://")
	if at < 0 || scheme < 0 || at < scheme {
		return u
	}
	if user, _, ok := strings.Cut(u[scheme+3:at], ":"); ok {
		return u[:scheme+3] + user + ":***" + u[at:]
	}
	return u
}
