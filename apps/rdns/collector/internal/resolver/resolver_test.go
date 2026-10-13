package resolver

import (
	"bufio"
	"context"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/miekg/dns"
)

// zone são as respostas de testdata/zone.txt.
type zone struct {
	rrs      map[string][]dns.RR // nome em minúsculas → registros
	behavior map[string]string   // nome → comportamento sintético (";!")
}

func loadZone(t *testing.T) *zone {
	t.Helper()
	f, err := os.Open("../../testdata/zone.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z := &zone{rrs: map[string][]dns.RR{}, behavior: map[string]string{}}
	zp := dns.NewZoneParser(f, ".", "zone.txt")
	for rr, ok := zp.Next(); ok; rr, ok = zp.Next() {
		name := strings.ToLower(rr.Header().Name)
		z.rrs[name] = append(z.rrs[name], rr)
	}
	if err := zp.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), ";! "); ok {
			if name, b, ok := strings.Cut(strings.TrimSpace(rest), " "); ok {
				z.behavior[strings.ToLower(name)] = strings.TrimSpace(b)
			}
		}
	}
	if len(z.rrs) == 0 || len(z.behavior) == 0 {
		t.Fatal("zone.txt sem registros ou sem comportamentos")
	}
	return z
}

// fakeServer é um resolvedor recursivo falso em 127.0.0.1, UDP e TCP na mesma
// porta. mode, quando não vazio, vale para toda pergunta: servfail, refused
// ou silent.
type fakeServer struct {
	addr string
	mode string
	zone *zone

	mu       sync.Mutex
	received []*dns.Msg
	nets     []string
}

func startServer(t *testing.T, z *zone, mode string) *fakeServer {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeServer{addr: pc.LocalAddr().String(), mode: mode, zone: z}
	for _, s := range []*dns.Server{{PacketConn: pc, Handler: f}, {Listener: ln, Handler: f}} {
		started := make(chan struct{})
		s.NotifyStartedFunc = func() { close(started) }
		go func() { _ = s.ActivateAndServe() }()
		<-started
		t.Cleanup(func() { _ = s.Shutdown() })
	}
	return f
}

func (f *fakeServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.received)
}

func (f *fakeServer) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	network := w.LocalAddr().Network()
	f.mu.Lock()
	f.received = append(f.received, req.Copy())
	f.nets = append(f.nets, network)
	f.mu.Unlock()

	q := req.Question[0]
	name := strings.ToLower(q.Name)
	mode := f.mode
	if mode == "" {
		mode = f.zone.behavior[name]
	}
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = true
	if req.IsEdns0() != nil {
		m.SetEdns0(UDPSize, false)
	}
	switch mode {
	case "silent":
		return
	case "servfail":
		m.Rcode = dns.RcodeServerFailure
	case "refused":
		m.Rcode = dns.RcodeRefused
	case "wrongq":
		m.Question[0].Name = "outra.pergunta.example."
		f.answer(m, name)
	case "truncate":
		if network == "udp" {
			m.Truncated = true
		} else {
			f.answer(m, name)
		}
	default:
		f.answer(m, name)
	}
	_ = w.WriteMsg(m)
}

// answer responde como um resolvedor recursivo: segue o CNAME e põe os PTR
// do nome final; nome desconhecido = NXDOMAIN; sem PTR = NODATA.
func (f *fakeServer) answer(m *dns.Msg, name string) {
	if _, ok := f.zone.rrs[name]; !ok {
		m.Rcode = dns.RcodeNameError
		return
	}
	for range 8 {
		next := ""
		for _, rr := range f.zone.rrs[name] {
			switch v := rr.(type) {
			case *dns.CNAME:
				m.Answer = append(m.Answer, rr)
				next = strings.ToLower(v.Target)
			case *dns.PTR:
				m.Answer = append(m.Answer, rr)
			}
		}
		if next == "" {
			return
		}
		name = next
	}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newResolver(retries int, clock *fakeClock, servers ...*fakeServer) *Resolver {
	o := Options{Timeout: 300 * time.Millisecond, Retries: retries, Inflight: 16}
	for _, s := range servers {
		o.Servers = append(o.Servers, s.addr)
	}
	if clock != nil {
		o.Now = clock.Now
	}
	return New(o)
}

func lookup(t *testing.T, r *Resolver, ip string) Result {
	t.Helper()
	res, err := r.Lookup(context.Background(), netip.MustParseAddr(ip), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAnswers(t *testing.T) {
	z := loadZone(t)
	r := newResolver(2, nil, startServer(t, z, ""))
	cases := []struct {
		ip     string
		status Status
		names  []string
	}{
		{"200.160.2.3", PTR, []string{"registro.br"}},
		{"200.160.2.1", PTR, []string{"ae1-0.ar1.vr.registro.br"}},
		// Dois nomes, em ordem de bytes; o primeiro vai em name.
		{"200.160.2.5", PTR, []string{"mailexploder.registro.br", "mailx.registro.br"}},
		{"200.160.2.2", NoPTR, nil},   // NXDOMAIN
		{"200.160.2.255", NoPTR, nil}, // NXDOMAIN
		{"192.0.2.4", NoPTR, nil},     // NODATA
		// RFC 2317: o PTR vem do nome para onde o CNAME aponta.
		{"64.62.200.2", PTR, []string{"ipv6.he.net"}},
		{"192.0.2.1", PTR, []string{"mail.example.net"}},
		{"192.0.2.2", PTR, []string{`caf\195\169.example.net`}},
		{"192.0.2.3", PTR, []string{"a.example.net"}},
		{"192.0.2.10", PTR, []string{`host\.um.example.net`}},
		{"192.0.2.11", PTR, []string{"."}},
	}
	for _, c := range cases {
		got := lookup(t, r, c.ip)
		if got.Status != c.status || !slices.Equal(got.Names, c.names) {
			t.Errorf("%s = %v %q, quero %v %q", c.ip, got.Status, got.Names, c.status, c.names)
		}
	}
	if got := lookup(t, r, "200.160.2.5"); got.Name() != "mailexploder.registro.br" {
		t.Errorf("Name = %q", got.Name())
	}
}

func TestQueryMessage(t *testing.T) {
	srv := startServer(t, loadZone(t), "")
	r := newResolver(0, nil, srv)
	lookup(t, r, "200.160.2.3")
	if srv.count() != 1 {
		t.Fatalf("consultas = %d", srv.count())
	}
	m := srv.received[0]
	q := m.Question[0]
	if q.Name != "3.2.160.200.in-addr.arpa." || q.Qtype != dns.TypePTR || q.Qclass != dns.ClassINET {
		t.Errorf("pergunta = %v", q)
	}
	if !m.RecursionDesired {
		t.Error("RD deveria estar ligado")
	}
	opt := m.IsEdns0()
	if opt == nil || opt.UDPSize() != 1232 || opt.Do() {
		t.Errorf("EDNS0 = %v", opt)
	}
	if srv.nets[0] != "udp" {
		t.Errorf("transporte = %s", srv.nets[0])
	}
	if r.Queries() != 1 {
		t.Errorf("Queries = %d", r.Queries())
	}
}

func TestServfail(t *testing.T) {
	z := loadZone(t)
	// SERVFAIL duas vezes → servfail, sem a terceira tentativa.
	a, b, c := startServer(t, z, "servfail"), startServer(t, z, "servfail"), startServer(t, z, "servfail")
	r := newResolver(2, nil, a, b, c)
	if got := lookup(t, r, "200.160.2.3"); got.Status != ServFail || got.Names != nil {
		t.Errorf("dois SERVFAIL = %+v", got)
	}
	if a.count() != 1 || b.count() != 1 || c.count() != 0 {
		t.Errorf("consultas = %d/%d/%d, quero 1/1/0", a.count(), b.count(), c.count())
	}

	// SERVFAIL uma vez e depois PTR → ptr.
	bad, good := startServer(t, z, "servfail"), startServer(t, z, "")
	r = newResolver(2, nil, bad, good)
	if got := lookup(t, r, "200.160.2.3"); got.Status != PTR || got.Name() != "registro.br" {
		t.Errorf("SERVFAIL e depois PTR = %+v", got)
	}

	// SERVFAIL por nome (zone.txt), em todos os servidores.
	r = newResolver(2, nil, startServer(t, z, ""), startServer(t, z, ""))
	if got := lookup(t, r, "192.0.2.6"); got.Status != ServFail {
		t.Errorf("192.0.2.6 = %+v", got)
	}
}

func TestExhausted(t *testing.T) {
	z := loadZone(t)
	for _, c := range []struct {
		mode string
		want Status
	}{{"refused", Refused}, {"silent", Timeout}} {
		a, b, s3 := startServer(t, z, c.mode), startServer(t, z, c.mode), startServer(t, z, c.mode)
		r := newResolver(2, nil, a, b, s3)
		if got := lookup(t, r, "200.160.2.3"); got.Status != c.want {
			t.Errorf("%s: %+v, quero %v", c.mode, got, c.want)
		}
		// Uma tentativa em cada servidor.
		if a.count() != 1 || b.count() != 1 || s3.count() != 1 {
			t.Errorf("%s: consultas = %d/%d/%d, quero 1/1/1", c.mode, a.count(), b.count(), s3.count())
		}
	}
	// Por nome, com um servidor só: as três tentativas vão a ele.
	srv := startServer(t, z, "")
	r := newResolver(2, nil, srv)
	if got := lookup(t, r, "192.0.2.7"); got.Status != Refused {
		t.Errorf("192.0.2.7 = %+v", got)
	}
	if srv.count() != 3 {
		t.Errorf("consultas = %d, quero 3", srv.count())
	}
	if got := lookup(t, r, "192.0.2.8"); got.Status != Timeout {
		t.Errorf("192.0.2.8 = %+v", got)
	}
}

func TestTruncatedRetriesTCP(t *testing.T) {
	srv := startServer(t, loadZone(t), "")
	r := newResolver(0, nil, srv)
	got := lookup(t, r, "192.0.2.5")
	if got.Status != PTR || got.Name() != "truncado.example.net" {
		t.Errorf("truncada = %+v", got)
	}
	if !slices.Equal(srv.nets, []string{"udp", "tcp"}) {
		t.Errorf("transportes = %v, quero udp e tcp", srv.nets)
	}
	if r.Queries() != 2 {
		t.Errorf("a repetição por TCP conta como outra consulta: %d", r.Queries())
	}
}

func TestWrongQuestionDiscarded(t *testing.T) {
	srv := startServer(t, loadZone(t), "")
	r := newResolver(1, nil, srv)
	// A resposta traz outra pergunta: vale como erro de rede (timeout).
	if got := lookup(t, r, "192.0.2.9"); got.Status != Timeout || got.Names != nil {
		t.Errorf("pergunta trocada = %+v", got)
	}
	if srv.count() != 2 {
		t.Errorf("consultas = %d, quero 2", srv.count())
	}
}

func TestServerHealth(t *testing.T) {
	z := loadZone(t)
	clock := &fakeClock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	bad, good := startServer(t, z, "refused"), startServer(t, z, "")
	r := newResolver(0, clock, bad, good) // uma tentativa por endereço

	// O rodízio alterna: as consultas ímpares vão ao servidor ruim. Na quinta
	// falha seguida ele sai do rodízio.
	for range 10 {
		lookup(t, r, "200.160.2.3")
	}
	if bad.count() != 5 || good.count() != 5 {
		t.Fatalf("consultas = %d/%d, quero 5/5", bad.count(), good.count())
	}
	for range 6 {
		if got := lookup(t, r, "200.160.2.3"); got.Status != PTR {
			t.Errorf("fora do rodízio, o bom deveria responder: %+v", got)
		}
	}
	if bad.count() != 5 {
		t.Errorf("o servidor fora do rodízio recebeu %d consultas", bad.count()-5)
	}

	// 30 s depois, volta ao rodízio.
	clock.Add(31 * time.Second)
	for range 4 {
		lookup(t, r, "200.160.2.3")
	}
	if bad.count() == 5 {
		t.Error("o servidor deveria voltar ao rodízio depois de 30 s")
	}
}

func TestAllDownUsesAll(t *testing.T) {
	z := loadZone(t)
	clock := &fakeClock{t: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)}
	a, b := startServer(t, z, "refused"), startServer(t, z, "refused")
	r := newResolver(0, clock, a, b)
	for range 10 {
		lookup(t, r, "200.160.2.3")
	}
	if a.count() != 5 || b.count() != 5 {
		t.Fatalf("consultas = %d/%d", a.count(), b.count())
	}
	// Os dois fora: usa os dois mesmo assim.
	for range 4 {
		if got := lookup(t, r, "200.160.2.3"); got.Status != Refused {
			t.Errorf("todos fora = %+v", got)
		}
	}
	if a.count() != 7 || b.count() != 7 {
		t.Errorf("todos fora: consultas = %d/%d, quero 7/7", a.count(), b.count())
	}
}

func TestValidAnswerResetsFailures(t *testing.T) {
	z := loadZone(t)
	clock := &fakeClock{t: time.Now()}
	srv := startServer(t, z, "")
	r := newResolver(0, clock, srv)
	// Quatro falhas, uma resposta válida (NXDOMAIN), mais quatro falhas: o
	// servidor nunca chega a cinco seguidas.
	for range 4 {
		lookup(t, r, "192.0.2.7")
	}
	lookup(t, r, "200.160.2.2")
	for range 4 {
		lookup(t, r, "192.0.2.7")
	}
	r.mu.Lock()
	down := r.servers[0].down
	r.mu.Unlock()
	if down {
		t.Error("NXDOMAIN deveria zerar a conta de falhas")
	}
}

func TestCanceled(t *testing.T) {
	srv := startServer(t, loadZone(t), "silent")
	r := newResolver(2, nil, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := r.Lookup(ctx, netip.MustParseAddr("200.160.2.3"), 0, nil); err == nil {
		t.Error("contexto encerrado deveria devolver erro")
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Registro.BR.":     "registro.br",
		".":                ".",
		`caf\195\169.Net.`: `caf\195\169.net`,
		`a\.B.example.`:    `a\.b.example`,
		"sem.ponto":        "sem.ponto",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, quero %q", in, got, want)
		}
	}
}
