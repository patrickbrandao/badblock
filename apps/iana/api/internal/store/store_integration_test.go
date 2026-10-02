//go:build integration

package store

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// setup sobe um PG18, aplica as migrations de central/ e iana/ (seção
// migrate:up), executa o seed (se houver) e devolve um Store conectado com o
// usuário postgres, como em produção, e a URL do banco.
func setup(t *testing.T, seedFile string) (*Store, string) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies(),
	)
	testcontainers.CleanupContainer(t, pg)
	if err != nil {
		t.Fatal(err)
	}
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	for _, dir := range []string{"central", "iana"} {
		files, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "..", "database", "postgres", dir, "*.sql"))
		if len(files) == 0 {
			t.Fatalf("nenhuma migration em database/postgres/%s", dir)
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			up, _, _ := strings.Cut(string(raw), "-- migrate:down")
			if _, err := admin.Exec(ctx, up); err != nil {
				t.Fatalf("%s: %v", f, err)
			}
		}
	}
	if seedFile != "" {
		raw, err := os.ReadFile(seedFile)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("%s: %v", seedFile, err)
		}
	}

	st, err := Open(ctx, url, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, url
}

// ipCase: bloco da IANA, blocos especiais (do mais específico para o menos),
// servidor RDAP e bogon esperados. "" = nenhum.
type ipCase struct {
	query   string
	block   string
	special string // prefixos separados por vírgula
	rdap    string
	bogon   bool
}

// Valem tanto para o recorte de testdata/seed.sql quanto para o dataset
// inteiro da IANA (TestRealDataset).
var ipCases = []ipCase{
	// Os casos da spec (specs/fontes/iana/api.md, "Regra de bogon").
	{"10.0.0.1/32", "10.0.0.0/8", "10.0.0.0/8", "", true},
	{"192.168.1.1/32", "192.0.0.0/8", "192.168.0.0/16", "192.0.0.0/8", true},
	{"100.64.0.1/32", "100.0.0.0/8", "100.64.0.0/10", "100.0.0.0/8", true},
	{"192.0.2.1/32", "192.0.0.0/8", "192.0.2.0/24", "192.0.0.0/8", true},
	{"8.8.8.8/32", "8.0.0.0/8", "", "8.0.0.0/8", false},
	{"187.87.29.10/32", "187.0.0.0/8", "", "187.0.0.0/8", false},
	{"2001:db8::1/128", "2001:c00::/23", "2001:db8::/32", "2001:c00::/23", true},
	{"fe80::1/128", "", "fe80::/10", "", true},
	{"2804:8ae0::1/128", "2800::/12", "", "2800::/12", false},
	{"::1/128", "", "::1/128", "", true},
	{"240.0.0.1/32", "240.0.0.0/8", "240.0.0.0/4", "", true},
	{"0.0.0.1/32", "0.0.0.0/8", "0.0.0.0/8", "", true},
	// Aninhamento, exceções, N/A e registros encerrados.
	{"0.0.0.0/32", "0.0.0.0/8", "0.0.0.0/32,0.0.0.0/8", "", true},
	{"127.0.0.1/32", "127.0.0.0/8", "127.0.0.0/8", "", true},
	{"192.0.0.9/32", "192.0.0.0/8", "192.0.0.9/32,192.0.0.0/24", "192.0.0.0/8", false},
	{"192.0.0.170/32", "192.0.0.0/8", "192.0.0.170/32,192.0.0.0/24", "192.0.0.0/8", true},
	{"192.88.99.1/32", "192.0.0.0/8", "192.88.99.0/24", "192.0.0.0/8", false},
	{"192.88.99.2/32", "192.0.0.0/8", "192.88.99.2/32,192.88.99.0/24", "192.0.0.0/8", true},
	{"224.0.0.1/32", "224.0.0.0/8", "", "", true},
	{"2001::1/128", "2001::/23", "2001::/32,2001::/23", "", false},
	{"2001:10::1/128", "2001::/23", "2001:10::/28,2001::/23", "", true},
	{"2001:20::1/128", "2001::/23", "2001:20::/28,2001::/23", "", false},
	{"2002::1/128", "2002::/16", "2002::/16", "", false},
	{"3fff::1/128", "3fff::/20", "3fff::/20", "", true},
	{"64:ff9b::1/128", "", "64:ff9b::/96", "", false},
	{"ff02::1/128", "", "", "", true},
	{"2000::1/128", "", "", "", true},
	// Prefixos.
	{"10.1.0.0/16", "10.0.0.0/8", "10.0.0.0/8", "", true},
	{"192.0.0.0/16", "192.0.0.0/8", "", "192.0.0.0/8", false},
	{"8.0.0.0/7", "", "", "", false},
	{"224.0.0.0/4", "", "", "", true},
	{"2001:db8::/48", "2001:c00::/23", "2001:db8::/32", "2001:c00::/23", true},
	{"ff00::/8", "", "", "", true},
	{"::/0", "", "", "", false},
}

func checkPrefixes(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	for _, c := range ipCases {
		l, err := st.Prefix(ctx, netip.MustParsePrefix(c.query))
		if err != nil {
			t.Errorf("%s: %v", c.query, err)
			continue
		}
		var block, rdap string
		var special []string
		if l.Block != nil {
			block = l.Block.Prefix.String()
		}
		for _, s := range l.Special {
			special = append(special, s.Prefix.String())
		}
		if l.RDAP != nil {
			rdap = l.RDAP.Resource
		}
		got := ipCase{c.query, block, strings.Join(special, ","), rdap, l.Bogon()}
		if got != c {
			t.Errorf("%s:\n got %+v\nquero %+v", c.query, got, c)
		}
	}

	// Colunas de um bloco ALLOCATED a um RIR (o do ASN de exemplo)...
	l, err := st.Prefix(ctx, netip.MustParsePrefix("187.87.29.10/32"))
	if err != nil {
		t.Fatal(err)
	}
	b := l.Block
	if b.Designation != "LACNIC" || *b.Registry != "lacnic" || *b.Whois != "whois.lacnic.net" || b.Status != "ALLOCATED" ||
		strings.Join(b.RDAPURLs, " ") != "https://rdap.lacnic.net/rdap/" || *b.AllocationDate != "2007-09" || b.Note != nil {
		t.Errorf("187/8 = %+v", b)
	}
	if l.RDAP.Kind != "ipv4" || *l.RDAP.Registry != "lacnic" || strings.Join(l.RDAP.URLs, " ") != "https://rdap.lacnic.net/rdap/" {
		t.Errorf("RDAP 187/8 = %+v", l.RDAP)
	}
	// ... e de um bloco LEGACY administrado pela ARIN.
	l, err = st.Prefix(ctx, netip.MustParsePrefix("8.8.8.8/32"))
	if err != nil {
		t.Fatal(err)
	}
	b = l.Block
	if b.Designation != "Administered by ARIN" || *b.Registry != "arin" || *b.Whois != "whois.arin.net" || b.Status != "LEGACY" ||
		strings.Join(b.RDAPURLs, " ") != "https://rdap.arin.net/registry http://rdap.arin.net/registry" || *b.AllocationDate != "1992-12" {
		t.Errorf("8/8 = %+v", b)
	}
	if l.RDAP.Kind != "ipv4" || *l.RDAP.Registry != "arin" || l.RDAP.URLs[0] != "https://rdap.arin.net/registry/" {
		t.Errorf("RDAP 8/8 = %+v", l.RDAP)
	}
	// Flags e datas do special registry (NULL = vazio ou N/A).
	l, err = st.Prefix(ctx, netip.MustParsePrefix("2001::1/128"))
	if err != nil {
		t.Fatal(err)
	}
	teredo := l.Special[0]
	if teredo.Name != "TEREDO" || teredo.GloballyReachable != nil || !*teredo.Source || *teredo.ReservedByProtocol ||
		*teredo.RFC != "[RFC4380] [RFC8190]" || *teredo.AllocationDate != "2006-01" || teredo.TerminationDate != nil {
		t.Errorf("TEREDO = %+v", teredo)
	}
	l, err = st.Prefix(ctx, netip.MustParsePrefix("192.88.99.1/32"))
	if err != nil {
		t.Fatal(err)
	}
	if s := l.Special[0]; *s.TerminationDate != "2015-03" || s.Source != nil || s.GloballyReachable != nil {
		t.Errorf("6to4 relay = %+v", s)
	}
}

type asnCase struct {
	asn     int64
	block   int64 // asn_start da faixa; -1 = nenhuma
	desc    string
	special string // faixas especiais "início-fim", separadas por vírgula
	rdap    string
}

var asnCases = []asnCase{
	{0, 0, "Reserved", "0-0", ""},
	{1, 1, "Assigned by ARIN", "", "1-1876"},
	{23456, 23456, "AS_TRANS", "23456-23456", ""},
	{61610, 61440, "Assigned by LACNIC", "", "61440-61951"},
	{64496, 64496, "Reserved for use in documentation and sample code", "64496-64511", ""},
	{65000, 64512, "Reserved for Private Use", "64512-65534", ""},
	{65535, 65535, "Reserved", "65535-65535", ""},
	{65551, 65536, "Reserved for use in documentation and sample code", "65536-65551", ""},
	{100000, 65552, "Reserved", "", ""},
	{262287, 262144, "Assigned by LACNIC", "", "262144-263167"},
	{300000, 275869, "Unallocated", "", ""},
	{4200000000, 4200000000, "Reserved for Private Use", "4200000000-4294967294", ""},
	{4294967295, 4294967295, "Reserved", "4294967295-4294967295", ""},
}

func checkASNs(t *testing.T, st *Store) {
	t.Helper()
	ctx := context.Background()
	for _, c := range asnCases {
		l, err := st.ASN(ctx, c.asn)
		if err != nil {
			t.Errorf("AS%d: %v", c.asn, err)
			continue
		}
		got := asnCase{asn: c.asn, block: -1}
		if l.Block != nil {
			got.block, got.desc = l.Block.Start, l.Block.Description
		}
		var special []string
		for _, s := range l.Special {
			special = append(special, strconv.FormatInt(s.Start, 10)+"-"+strconv.FormatInt(s.End, 10))
		}
		got.special = strings.Join(special, ",")
		if l.RDAP != nil {
			got.rdap = l.RDAP.Resource
		}
		if got != c {
			t.Errorf("AS%d:\n got %+v\nquero %+v", c.asn, got, c)
		}
	}
	l, err := st.ASN(ctx, 61610)
	if err != nil {
		t.Fatal(err)
	}
	if b := l.Block; b.End != 61951 || *b.Registry != "lacnic" || *b.Whois != "whois.lacnic.net" ||
		b.RDAPURLs[0] != "https://rdap.lacnic.net/rdap/" || *b.RegistrationDate != "2013-06-11" || b.Reference != nil {
		t.Errorf("AS61610 = %+v", b)
	}
}

func TestQueries(t *testing.T) {
	st, _ := setup(t, filepath.Join("..", "..", "testdata", "seed.sql"))
	ctx := context.Background()

	// Versão: a última aplicada (status = 1), não a recusada mais nova.
	d, err := st.Dataset(ctx)
	if err != nil || d == nil {
		t.Fatalf("Dataset = %+v, err = %v", d, err)
	}
	if d.Version != "01a0eaa2-fd5c-7c24-bc6c-4e3c35d61c53" || d.SHA256 != "b22ef6b2e76eafd675b5d1a144ad6488720da7c39d56e1d8c09a6cf6401ad06e" ||
		len(d.Files) != 10 || d.Files[0].Name != "as-numbers-1" || *d.Files[0].Rows != 88 || d.Files[0].ETag != nil ||
		*d.Files[7].ETag != `W/"1138-65336a3cb9688-br"` || d.Files[0].Publication != nil {
		t.Errorf("Dataset = %+v", d)
	}
	if p := d.Publication(FileRDAPIPv4); p == nil || *p != "2019-06-07T19:00:02Z" {
		t.Errorf("publication ipv4 = %v", p)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, err = %v", j, err)
	}

	checkPrefixes(t, st)
	checkASNs(t, st)

	asns, err := st.ASNBlocks(ctx)
	if err != nil || len(asns) != 13 || asns[0].Start != 0 || asns[12].Start != 4294967295 {
		t.Errorf("ASNBlocks = %d, err = %v", len(asns), err)
	}
	v4, err := st.PrefixBlocks(ctx, 4)
	if err != nil || len(v4) != 9 || v4[0].Prefix.String() != "0.0.0.0/8" || v4[8].Prefix.String() != "240.0.0.0/8" {
		t.Errorf("PrefixBlocks(4) = %v, err = %v", v4, err)
	}
	v6, err := st.PrefixBlocks(ctx, 6)
	if err != nil || len(v6) != 6 || v6[0].Prefix.String() != "2001::/23" || v6[5].Prefix.String() != "3fff::/20" {
		t.Errorf("PrefixBlocks(6) = %v, err = %v", v6, err)
	}
	sp, err := st.Special(ctx)
	if err != nil || len(sp.Prefixes) != 51 || len(sp.ASNs) != 9 || sp.Prefixes[0].Prefix.String() != "0.0.0.0/8" ||
		sp.Prefixes[25].Prefix.String() != "255.255.255.255/32" || sp.Prefixes[26].Prefix.String() != "::/128" {
		t.Errorf("Special = %+v, err = %v", sp, err)
	}
	rd, err := st.RDAP(ctx)
	if err != nil || len(rd.Services) != 9 || rd.Services[0].Resource != "1-1876" || rd.Services[3].Resource != "8.0.0.0/8" ||
		rd.Services[8].Resource != "2800::/12" || *rd.Publication[FileRDAPASN] != "2026-06-01T20:00:01Z" {
		t.Errorf("RDAP = %+v, err = %v", rd, err)
	}
}

func TestEmptyDatabase(t *testing.T) {
	st, _ := setup(t, "")
	ctx := context.Background()
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Errorf("Dataset vazio = %+v, %v", d, err)
	}
	if j, err := st.Job(ctx); j != nil || err != nil {
		t.Errorf("Job vazio = %+v, %v", j, err)
	}
	l, err := st.Prefix(ctx, netip.MustParsePrefix("10.0.0.1/32"))
	if err != nil || l.Block != nil || len(l.Special) != 0 || l.RDAP != nil || l.Unreserved {
		t.Errorf("Prefix vazio = %+v, %v", l, err)
	}
	a, err := st.ASN(ctx, 1)
	if err != nil || a.Block != nil || a.RDAP != nil {
		t.Errorf("ASN vazio = %+v, %v", a, err)
	}
	rd, err := st.RDAP(ctx)
	if err != nil || len(rd.Services) != 0 || rd.Publication[FileRDAPASN] != nil {
		t.Errorf("RDAP vazio = %+v, %v", rd, err)
	}
}

// TestRealDataset carrega o dataset inteiro da IANA com o próprio binário do
// collector-iana (o contrato entre os apps é o banco) servindo os 10 arquivos
// de IANA_REAL_DIR num httptest.Server, e confere as mesmas consultas.
// make test-real baixa os arquivos do dia.
func TestRealDataset(t *testing.T) {
	dir := os.Getenv("IANA_REAL_DIR")
	if dir == "" {
		t.Skip("defina IANA_REAL_DIR com os 10 arquivos da IANA (make test-real)")
	}
	st, url := setup(t, "")

	bin := filepath.Join(t.TempDir(), "collector-iana")
	build := exec.Command("go", "build", "-o", bin, "./cmd/collector-iana")
	build.Dir = filepath.Join("..", "..", "..", "collector")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build do collector-iana: %v\n%s", err, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(dir, path.Base(r.URL.Path)))
	}))
	defer srv.Close()
	run := exec.Command(bin, "--once", "--postgres-url", url, "--log-format", "text",
		"--iana-base-url", srv.URL+"/assignments", "--rdap-base-url", srv.URL+"/rdap")
	if out, err := run.CombinedOutput(); err != nil {
		t.Fatalf("collector-iana --once: %v\n%s", err, out)
	}

	ctx := context.Background()
	d, err := st.Dataset(ctx)
	if err != nil || d == nil || len(d.Files) != 10 {
		t.Fatalf("Dataset = %+v, err = %v", d, err)
	}
	for _, name := range []string{FileRDAPASN, FileRDAPIPv4, FileRDAPIPv6} {
		if d.Publication(name) == nil {
			t.Errorf("sem publication em %s", name)
		}
	}
	checkPrefixes(t, st)
	checkASNs(t, st)

	// Contagens mínimas do dataset inteiro (collector-iana, specs/fontes/iana/fonte.md).
	asns, _ := st.ASNBlocks(ctx)
	v4, _ := st.PrefixBlocks(ctx, 4)
	v6, _ := st.PrefixBlocks(ctx, 6)
	sp, _ := st.Special(ctx)
	rd, _ := st.RDAP(ctx)
	if len(asns) < 150 || len(v4) != 256 || len(v6) < 30 || len(sp.Prefixes) < 30 || len(sp.ASNs) < 6 || len(rd.Services) < 270 {
		t.Errorf("contagens: asn %d, ipv4 %d, ipv6 %d, special %d/%d, rdap %d",
			len(asns), len(v4), len(v6), len(sp.Prefixes), len(sp.ASNs), len(rd.Services))
	}
	t.Logf("dataset real: asn %d, ipv4 %d, ipv6 %d, special %d/%d, rdap %d",
		len(asns), len(v4), len(v6), len(sp.Prefixes), len(sp.ASNs), len(rd.Services))
}
