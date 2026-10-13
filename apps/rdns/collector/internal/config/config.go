// Package config lê a configuração do collector-rdns. Cada opção assume, em
// ordem: o valor padrão, a variável de ambiente e o argumento de linha de
// comando.
package config

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração completa.
type Config struct {
	PostgresURL    string
	RedisURL       string // vazio = sem tempo real
	Sources        []string
	DeriveInterval time.Duration
	RunTimeout     time.Duration
	ScanInterval   time.Duration // reserva de rdns_config.scan_interval
	ScanLease      time.Duration
	RateLimit      int      // reserva de rdns_config.rate_limit
	DNSServers     []string // host:porta
	DNSTimeout     time.Duration
	DNSRetries     int
	DNSInflight    int
	LogLevel       string
	LogFormat      string

	Once        bool
	Force       bool
	ShowVersion bool
}

type option struct {
	flag, env, def, usage string
}

var options = []option{
	{"postgres-url", "POSTGRES_URL", "", "URL do Postgres, ex.: postgres://postgres:senha@badblock-postgres:5432/badblock (obrigatória)"},
	{"redis-url", "REDIS_URL", "", "URL do Valkey, ex.: redis://:senha@badblock-valkey:6379/0 (vazio desliga o tempo real)"},
	{"dns-servers", "DNS_SERVERS", "", "resolvedores recursivos deste host, ip ou ip:porta ([ip]:porta no IPv6), separados por vírgula (obrigatória)"},
	{"dns-timeout", "DNS_TIMEOUT", "2s", "prazo de uma tentativa de consulta DNS"},
	{"dns-retries", "DNS_RETRIES", "2", "novas tentativas de uma consulta, cada uma noutro servidor (0 a 5)"},
	{"dns-inflight", "DNS_INFLIGHT", "512", "consultas DNS esperando resposta ao mesmo tempo, por instância"},
	{"rate-limit", "RATE_LIMIT", "256", "consultas DNS por segundo desta instância quando rdns_config não tem rate_limit"},
	{"scan-interval", "SCAN_INTERVAL", "24h", "meta por prefixo quando rdns_config não tem scan_interval"},
	{"scan-lease", "SCAN_LEASE", "2m", "prazo de um prefixo pego por esta instância (mínimo 30s)"},
	{"sources", "SOURCES", "cgibr", "origem dos prefixos derivados: cgibr ou none (só os estáticos)"},
	{"derive-interval", "DERIVE_INTERVAL", "5m", "intervalo entre passadas da derivação"},
	{"run-timeout", "RUN_TIMEOUT", "10m", "tempo máximo de uma passada da derivação"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("collector-rdns", flag.ContinueOnError)
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
		{"dns-timeout", &c.DNSTimeout}, {"scan-interval", &c.ScanInterval}, {"scan-lease", &c.ScanLease},
		{"derive-interval", &c.DeriveInterval}, {"run-timeout", &c.RunTimeout},
	} {
		v := *values[d.flag]
		if *d.dst, err = time.ParseDuration(v); err != nil || *d.dst <= 0 {
			if d.flag == "scan-lease" {
				return nil, fmt.Errorf("--scan-lease inválido (mínimo 30s): %q", v)
			}
			return nil, fmt.Errorf("--%s inválido: %q", d.flag, v)
		}
	}
	if c.ScanLease < 30*time.Second {
		return nil, fmt.Errorf("--scan-lease inválido (mínimo 30s): %q", *values["scan-lease"])
	}
	if c.DNSRetries, err = strconv.Atoi(*values["dns-retries"]); err != nil || c.DNSRetries < 0 || c.DNSRetries > 5 {
		return nil, fmt.Errorf("--dns-retries inválido (0 a 5): %q", *values["dns-retries"])
	}
	if c.DNSInflight, err = strconv.Atoi(*values["dns-inflight"]); err != nil || c.DNSInflight < 1 {
		return nil, fmt.Errorf("--dns-inflight inválido: %q", *values["dns-inflight"])
	}
	if c.RateLimit, err = strconv.Atoi(*values["rate-limit"]); err != nil || c.RateLimit < 1 {
		return nil, fmt.Errorf("--rate-limit inválido: %q", *values["rate-limit"])
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
	if c.DNSServers, err = ParseServers(*values["dns-servers"]); err != nil {
		return nil, err
	}
	if c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

// ParseServers lê a lista de DNS_SERVERS: ip, ip:porta ou [ip]:porta (IPv6),
// separados por vírgula; sem porta, vale a 53. Devolve cada um como host:porta.
func ParseServers(list string) ([]string, error) {
	var out []string
	for item := range strings.SplitSeq(list, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if ip, err := netip.ParseAddr(item); err == nil && ip.Zone() == "" {
			out = append(out, net.JoinHostPort(ip.Unmap().String(), "53"))
			continue
		}
		ap, err := netip.ParseAddrPort(item)
		if err != nil || ap.Port() == 0 || ap.Addr().Zone() != "" {
			return nil, fmt.Errorf("servidor DNS inválido %q", item)
		}
		out = append(out, net.JoinHostPort(ap.Addr().Unmap().String(), strconv.Itoa(int(ap.Port()))))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("defina DNS_SERVERS (ou --dns-servers)")
	}
	return out, nil
}

// PrintHelp escreve o texto de ajuda.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `collector-rdns — varre o DNS reverso (PTR) dos IPv4 cadastrados em
rdns_prefix para as tabelas rdns_* do PostgreSQL do BadBlock.

Sonda ativa: deriva os prefixos dos blocos IPv4 do cgibr (quebrados em /24),
pega um prefixo de cada vez da fila (com lease, várias instâncias dividem a
fila), pergunta o PTR de cada endereço aos resolvedores de DNS_SERVERS e grava
só o que mudou. Com REDIS_URL, atende também os pedidos de tempo real da
api-rdns. Não tem API HTTP.

Uso:
  collector-rdns [opções]

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
	fmt.Fprint(w, `  --once                 faz uma passada da derivação e sai
  --force                deriva mesmo sem versão nova da origem (implica --once)
  --version              mostra a versão e sai
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
