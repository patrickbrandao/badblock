// Package config lê a configuração do collector-registrobr-rdapnum. Cada
// opção assume, em ordem: o valor padrão, a variável de ambiente e o
// argumento de linha de comando.
package config

import (
	"flag"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração completa.
type Config struct {
	PostgresURL         string
	RedisURL            string // vazio = sem atualização pedida pela API
	RDAPURL             string // sem a barra final
	ProxyURL            *url.URL
	HTTPTimeout         time.Duration
	RateLimit           int           // reserva de registrobr_rdapnum_config.rate_limit, por minuto
	ScanInterval        time.Duration // reserva de registrobr_rdapnum_config.scan_interval
	ScanLease           time.Duration
	TruncatedPauseAfter int
	TruncatedPause      time.Duration
	ProxySameIPWarn     int
	Sources             []string
	DeriveInterval      time.Duration
	RunTimeout          time.Duration
	LogLevel            string
	LogFormat           string

	Once        bool
	Force       bool
	ShowVersion bool
}

// SafeRateWithoutProxy é a taxa (por minuto) acima da qual, sem proxy, as
// respostas vêm truncadas (fonte.md#limite-do-servidor).
const SafeRateWithoutProxy = 8

// RateNeedsProxy diz se a taxa pede o aviso "taxa acima de 8/min sem proxy".
func RateNeedsProxy(rate int, proxy bool) bool {
	return !proxy && rate > SafeRateWithoutProxy
}

// UserAgent é o User-Agent das consultas (padrão de nomes).
func UserAgent(version string) string {
	return "badblock-collector-registrobr-rdapnum/" + version + " (+https://github.com/patrickbrandao/badblock)"
}

type option struct {
	flag, env, def, usage string
}

var options = []option{
	{"postgres-url", "POSTGRES_URL", "", "URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)"},
	{"redis-url", "REDIS_URL", "", "URL do Valkey, ex.: redis://:senha@badblock-valkey:6379/0 (vazio desliga a atualização pedida pela API)"},
	{"rdap-url", "RDAP_URL", "https://rdap.registro.br", "base das consultas RDAP (http:// só para teste)"},
	{"proxy-url", "PROXY_URL", "", "proxy de saída que roda o IP: http://, https:// ou socks5://, com usuário:senha@ se pedir (vazio = saída direta)"},
	{"http-timeout", "HTTP_TIMEOUT", "20s", "prazo de uma consulta inteira"},
	{"rate-limit", "RATE_LIMIT", "32", "consultas por MINUTO desta instância quando registrobr_rdapnum_config não tem rate_limit"},
	{"scan-interval", "SCAN_INTERVAL", "168h", "meta por alvo quando registrobr_rdapnum_config não tem scan_interval"},
	{"scan-lease", "SCAN_LEASE", "2m", "prazo de um alvo pego por esta instância (mínimo 30s e maior que --http-timeout + 10s)"},
	{"truncated-pause-after", "TRUNCATED_PAUSE_AFTER", "3", "respostas truncadas seguidas que pausam a instância (>= 1)"},
	{"truncated-pause", "TRUNCATED_PAUSE", "2m", "duração da pausa depois das truncadas seguidas"},
	{"proxy-same-ip-warn", "PROXY_SAME_IP_WARN", "10", "respostas seguidas com o mesmo IP de saída que geram aviso, com proxy (0 desliga)"},
	{"sources", "SOURCES", "cgibr", "origem dos alvos derivados: cgibr ou none (só os estáticos)"},
	{"derive-interval", "DERIVE_INTERVAL", "5m", "intervalo entre passadas da derivação"},
	{"run-timeout", "RUN_TIMEOUT", "10m", "tempo máximo de uma passada da derivação"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("collector-registrobr-rdapnum", flag.ContinueOnError)
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
	fs.BoolVar(&c.Once, "once", false, "faz uma passada da derivação e sai")
	fs.BoolVar(&c.Force, "force", false, "deriva mesmo sem versão nova da origem (implica --once)")
	fs.BoolVar(&c.ShowVersion, "version", false, "mostra a versão e sai")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumento inesperado: %s", fs.Arg(0))
	}
	if c.Force {
		c.Once = true
	}

	var err error
	c.PostgresURL = *values["postgres-url"]
	c.RedisURL = *values["redis-url"]
	c.LogLevel = strings.ToLower(*values["log-level"])
	c.LogFormat = strings.ToLower(*values["log-format"])

	for _, d := range []struct {
		flag string
		dst  *time.Duration
	}{
		{"http-timeout", &c.HTTPTimeout}, {"scan-interval", &c.ScanInterval}, {"scan-lease", &c.ScanLease},
		{"truncated-pause", &c.TruncatedPause}, {"derive-interval", &c.DeriveInterval}, {"run-timeout", &c.RunTimeout},
	} {
		v := *values[d.flag]
		if *d.dst, err = time.ParseDuration(v); err != nil || *d.dst <= 0 {
			if d.flag == "scan-lease" {
				return nil, leaseErr(v)
			}
			return nil, fmt.Errorf("--%s inválido: %q", d.flag, v)
		}
	}
	if c.ScanLease < 30*time.Second || c.ScanLease <= c.HTTPTimeout+10*time.Second {
		return nil, leaseErr(*values["scan-lease"])
	}
	if c.RateLimit, err = strconv.Atoi(*values["rate-limit"]); err != nil || c.RateLimit < 1 {
		return nil, fmt.Errorf("--rate-limit inválido: %q", *values["rate-limit"])
	}
	if c.TruncatedPauseAfter, err = strconv.Atoi(*values["truncated-pause-after"]); err != nil || c.TruncatedPauseAfter < 1 {
		return nil, fmt.Errorf("--truncated-pause-after inválido: %q", *values["truncated-pause-after"])
	}
	if c.ProxySameIPWarn, err = strconv.Atoi(*values["proxy-same-ip-warn"]); err != nil || c.ProxySameIPWarn < 0 {
		return nil, fmt.Errorf("--proxy-same-ip-warn inválido: %q", *values["proxy-same-ip-warn"])
	}
	if c.RDAPURL, err = parseRDAPURL(*values["rdap-url"]); err != nil {
		return nil, err
	}
	if c.ProxyURL, err = ParseProxyURL(*values["proxy-url"]); err != nil {
		return nil, err
	}
	switch s := strings.ToLower(strings.TrimSpace(*values["sources"])); s {
	case "cgibr":
		c.Sources = []string{"cgibr"}
	case "none":
		c.Sources = nil
	default:
		return nil, fmt.Errorf("--sources inválido: %q (use cgibr ou none)", *values["sources"])
	}
	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		return nil, fmt.Errorf("--log-level inválido: %q", c.LogLevel)
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return nil, fmt.Errorf("--log-format inválido: %q", c.LogFormat)
	}
	if c.ShowVersion {
		return c, nil
	}
	if c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

func leaseErr(v string) error {
	return fmt.Errorf("--scan-lease inválido (mínimo 30s e maior que --http-timeout + 10s): %q", v)
}

func parseRDAPURL(v string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("--rdap-url inválida: %q", v)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// ParseProxyURL valida a PROXY_URL (vazia = sem proxy). A mensagem de erro
// nunca ecoa o valor: ele leva a senha do proxy.
func ParseProxyURL(v string) (*url.URL, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, fmt.Errorf("--proxy-url inválida (use http, https ou socks5)")
	}
	switch u.Scheme {
	case "http", "https", "socks5":
	default:
		return nil, fmt.Errorf("--proxy-url inválida (use http, https ou socks5)")
	}
	return u, nil
}

// RedactProxy mostra a PROXY_URL para o log, com a senha trocada por xxxxx
// (none sem proxy).
func RedactProxy(u *url.URL) string {
	if u == nil {
		return "none"
	}
	return u.Redacted()
}

// PrintHelp escreve o texto de ajuda.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `collector-registrobr-rdapnum — consulta o RDAP do registro.br de cada ASN e
bloco brasileiro para as tabelas registrobr_rdapnum_* do PostgreSQL do BadBlock.

Sonda ativa: deriva os alvos dos ASNs e blocos do cgibr, pega um alvo de cada
vez da fila (com lease, várias instâncias dividem a fila), consulta
/autnum/<asn> ou /ip/<bloco> na taxa de rate_limit por minuto, guarda a
resposta crua com histórico e a processa em titular, contatos, blocos e
delegações reversas. Resposta truncada pelo servidor é guardada, mas nunca
processada. Com REDIS_URL, atende também os pedidos de atualização da
api-registrobr-rdapnum. Não tem API HTTP.

Uso:
  collector-registrobr-rdapnum [opções]

Opções (padrão → variável de ambiente → argumento):
`)
	sorted := append([]option(nil), options...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].flag < sorted[j].flag })
	for _, o := range sorted {
		def := o.def
		if def == "" {
			def = `""`
		}
		fmt.Fprintf(w, "  --%-22s %s\n  %24s env %s, padrão %s\n", o.flag, o.usage, "", o.env, def)
	}
	fmt.Fprint(w, `  --once                   faz uma passada da derivação e sai
  --force                  deriva mesmo sem versão nova da origem (implica --once)
  --version                mostra a versão e sai
  -h, --help               mostra esta ajuda
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
