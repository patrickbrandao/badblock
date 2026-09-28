// Package config lê a configuração do registry-sync. Cada opção assume, em
// ordem: o valor padrão, a variável de ambiente e o argumento de linha de
// comando.
package config

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config é a configuração completa.
type Config struct {
	PostgresURL      string
	HTTPPort         int
	DataDir          string
	SyncInterval     time.Duration
	SourceIntervals  map[string]time.Duration
	Sources          []string
	SourcesDir       string
	RemovalThreshold float64
	RemovalGrace     time.Duration
	RawKeep          int
	RunRetention     time.Duration
	HTTPTimeout      time.Duration
	MaxDownloadBytes int64
	LogLevel         string
	LogFormat        string

	Once        bool
	Rebuild     bool
	Force       bool
	ShowVersion bool
}

// option descreve uma opção para o parser e para o texto de ajuda.
type option struct {
	flag, env, def, usage string
}

var options = []option{
	{"postgres-url", "POSTGRES_URL", "", "URL do Postgres com o role badblock_sync (obrigatória)"},
	{"http-port", "HTTP_PORT", "8002", "porta dos endpoints /health, /status e /ping"},
	{"data-dir", "DATA_DIR", "/data", "diretório dos arquivos brutos aplicados"},
	{"sync-interval", "SYNC_INTERVAL", "1h", "intervalo padrão entre verificações de cada fonte"},
	{"source-intervals", "SYNC_SOURCE_INTERVALS", "", "intervalos por fonte, ex.: iana-ipv4=6h,asnames=2h"},
	{"sources", "SYNC_SOURCES", "", "fontes a sincronizar, separadas por vírgula (vazio = todas)"},
	{"sources-dir", "SOURCES_DIR", "", "lê as fontes de <dir>/<id> em vez da rede (testes e e2e)"},
	{"removal-threshold", "REMOVAL_THRESHOLD", "0.05", "fração máxima de linhas que uma fonte pode remover numa carga"},
	{"removal-grace", "REMOVAL_GRACE", "26h", "carência até um recurso ausente de todas as fontes ser removido"},
	{"raw-keep", "RAW_KEEP", "7", "quantos arquivos brutos guardar por fonte (0 desliga)"},
	{"run-retention", "RUN_RETENTION", "4320h", "por quanto tempo manter ingest.source_run"},
	{"http-timeout", "HTTP_TIMEOUT", "5m", "tempo máximo de cada download"},
	{"max-download-bytes", "MAX_DOWNLOAD_BYTES", "268435456", "tamanho máximo de um arquivo baixado"},
	{"log-level", "LOG_LEVEL", "info", "debug, info, warn ou error"},
	{"log-format", "LOG_FORMAT", "json", "json ou text"},
}

// Load interpreta os argumentos (sem o nome do programa) e o ambiente.
func Load(args []string, getenv func(string) string, stderr io.Writer) (*Config, error) {
	fs := flag.NewFlagSet("registry-sync", flag.ContinueOnError)
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
	var sourceAlias string
	c := &Config{}
	fs.StringVar(&sourceAlias, "source", "", "alias de --sources")
	fs.BoolVar(&c.Once, "once", false, "roda um ciclo com todas as fontes selecionadas e sai")
	fs.BoolVar(&c.Rebuild, "rebuild", false, "só reconstrói o central e sai")
	fs.BoolVar(&c.Force, "force", false, "ignora a trava de remoção e a checagem de arquivo antigo")
	fs.BoolVar(&c.ShowVersion, "version", false, "mostra a versão e sai")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("argumento inesperado: %s", fs.Arg(0))
	}

	var err error
	c.PostgresURL = *values["postgres-url"]
	c.DataDir = *values["data-dir"]
	c.SourcesDir = *values["sources-dir"]
	c.LogLevel = *values["log-level"]
	c.LogFormat = *values["log-format"]
	if c.HTTPPort, err = strconv.Atoi(*values["http-port"]); err != nil {
		return nil, fmt.Errorf("--http-port: %w", err)
	}
	if c.SyncInterval, err = time.ParseDuration(*values["sync-interval"]); err != nil || c.SyncInterval <= 0 {
		return nil, fmt.Errorf("--sync-interval inválido: %q", *values["sync-interval"])
	}
	if c.SourceIntervals, err = parseIntervals(*values["source-intervals"]); err != nil {
		return nil, fmt.Errorf("--source-intervals: %w", err)
	}
	sources := *values["sources"]
	if sourceAlias != "" {
		sources = sourceAlias
	}
	for _, s := range strings.Split(sources, ",") {
		if s = strings.TrimSpace(s); s != "" {
			c.Sources = append(c.Sources, s)
		}
	}
	if c.RemovalThreshold, err = strconv.ParseFloat(*values["removal-threshold"], 64); err != nil ||
		c.RemovalThreshold < 0 || c.RemovalThreshold > 1 {
		return nil, fmt.Errorf("--removal-threshold deve ficar entre 0 e 1: %q", *values["removal-threshold"])
	}
	if c.RemovalGrace, err = time.ParseDuration(*values["removal-grace"]); err != nil || c.RemovalGrace < 0 {
		return nil, fmt.Errorf("--removal-grace inválido: %q", *values["removal-grace"])
	}
	if c.RawKeep, err = strconv.Atoi(*values["raw-keep"]); err != nil || c.RawKeep < 0 {
		return nil, fmt.Errorf("--raw-keep inválido: %q", *values["raw-keep"])
	}
	if c.RunRetention, err = time.ParseDuration(*values["run-retention"]); err != nil {
		return nil, fmt.Errorf("--run-retention inválido: %q", *values["run-retention"])
	}
	if c.HTTPTimeout, err = time.ParseDuration(*values["http-timeout"]); err != nil || c.HTTPTimeout <= 0 {
		return nil, fmt.Errorf("--http-timeout inválido: %q", *values["http-timeout"])
	}
	if c.MaxDownloadBytes, err = strconv.ParseInt(*values["max-download-bytes"], 10, 64); err != nil || c.MaxDownloadBytes <= 0 {
		return nil, fmt.Errorf("--max-download-bytes inválido: %q", *values["max-download-bytes"])
	}
	if !c.ShowVersion && c.PostgresURL == "" {
		return nil, fmt.Errorf("defina POSTGRES_URL (ou --postgres-url)")
	}
	return c, nil
}

// parseIntervals lê "id=duração,id=duração".
func parseIntervals(v string) (map[string]time.Duration, error) {
	out := map[string]time.Duration{}
	for _, item := range strings.Split(v, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		id, d, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("item %q sem '='", item)
		}
		dur, err := time.ParseDuration(strings.TrimSpace(d))
		if err != nil || dur <= 0 {
			return nil, fmt.Errorf("duração inválida em %q", item)
		}
		out[strings.TrimSpace(id)] = dur
	}
	return out, nil
}

// PrintHelp escreve o texto de ajuda: propósito, argumentos com o valor padrão
// e a variável de ambiente de cada um.
func PrintHelp(w io.Writer) {
	fmt.Fprint(w, `registry-sync — importa e sincroniza os dados públicos de ASNs e prefixos IP
(IANA, RIRs, NIC.br e asn.txt do RIPE) com o PostgreSQL do BadBlock.

Sem argumentos, roda como serviço: verifica cada fonte no seu intervalo (GET
condicional), aplica o que mudou e reconstrói as tabelas centrais.

Uso:
  registry-sync [opções]
  registry-sync --once [--source=rir-lacnic,nicbr] [--force]
  registry-sync --rebuild
  registry-sync healthcheck

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
	fmt.Fprint(w, `  --once                 roda um ciclo com todas as fontes selecionadas e sai
  --source               alias de --sources
  --rebuild              só reconstrói o central e sai
  --force                ignora a trava de remoção e a checagem de arquivo antigo
  --version              mostra a versão e sai
  -h, --help             mostra esta ajuda
`)
}

// Getenv é o os.Getenv, separado para os testes.
var Getenv = os.Getenv
