package rdap

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeRDAP é um servidor RDAP falso que serve as fixtures de testdata/.
type fakeRDAP struct {
	t       *testing.T
	mu      sync.Mutex
	headers http.Header
}

func (f *fakeRDAP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.headers = r.Header.Clone()
	f.mu.Unlock()
	serve := func(name string) {
		w.Header().Set("Content-Type", "application/rdap+json")
		_, _ = w.Write(fixture(f.t, name))
	}
	switch r.URL.Path {
	case "/autnum/61610":
		serve("autnum-61610.json")
	case "/autnum/61611":
		serve("autnum-61610-truncated.json")
	case "/autnum/64500":
		serve("synthetic-invalid.json")
	case "/autnum/64501":
		serve("synthetic-wrong-class.json")
	case "/autnum/64502":
		w.WriteHeader(http.StatusTooManyRequests)
	case "/autnum/64503":
		w.WriteHeader(http.StatusInternalServerError)
	case "/autnum/64504":
		time.Sleep(500 * time.Millisecond)
		serve("autnum-61610.json")
	case "/autnum/64505":
		_, _ = w.Write([]byte(`{"objectClassName":"autnum","x":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", MaxBody)))
		_, _ = w.Write([]byte(`"}`))
	case "/autnum/64506":
		http.Redirect(w, r, "/autnum/61610", http.StatusFound)
	case "/autnum/64507":
		http.Redirect(w, r, "/autnum/64507", http.StatusFound)
	case "/ip/200.225.48.0/24":
		serve("ip-200.225.48.0-21.json")
	case "/ip/2804:8ae0::/32":
		serve("ip-2804-8ae0--32.json")
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newClient(t *testing.T, timeout time.Duration) (*Client, *fakeRDAP) {
	t.Helper()
	f := &fakeRDAP{t: t}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(Options{BaseURL: srv.URL, Timeout: timeout, UserAgent: "badblock-collector-registrobr-rdapnum/test"}), f
}

func TestClassify(t *testing.T) {
	c, f := newClient(t, 200*time.Millisecond)
	ctx := context.Background()
	cases := []struct {
		kind, target string
		want         Result
		status       int
		errHas       string
	}{
		{KindASN, "61610", Complete, 200, ""},
		{KindASN, "61611", Truncated, 200, ""},
		{KindASN, "99999", NotFound, 404, ""},
		{KindASN, "64500", Error, 200, "JSON inválido"},
		{KindASN, "64501", Error, 200, "objectClassName"},
		{KindASN, "64502", Error, 429, "HTTP 429"},
		{KindASN, "64503", Error, 500, "HTTP 500"},
		{KindASN, "64504", Error, 0, "deadline"},
		{KindASN, "64505", Error, 200, "1 MiB"},
		{KindASN, "64506", Complete, 200, ""},
		{KindASN, "64507", Error, 0, "redirecionamentos"},
		{KindPrefix, "200.225.48.0/24", Complete, 200, ""},
		{KindPrefix, "2804:8ae0::/32", Complete, 200, ""},
		{KindPrefix, "8.8.8.0/24", NotFound, 404, ""},
	}
	for _, tc := range cases {
		r := c.Fetch(ctx, tc.kind, tc.target)
		if r.Result != tc.want || r.HTTPStatus != tc.status {
			t.Errorf("%s %s = %v/%d (%v), quero %v/%d", tc.kind, tc.target, r.Result, r.HTTPStatus, r.Err, tc.want, tc.status)
			continue
		}
		if tc.errHas != "" && (r.Err == nil || !strings.Contains(r.Err.Error(), tc.errHas)) {
			t.Errorf("%s: err = %v, quero %q", tc.target, r.Err, tc.errHas)
		}
		if tc.want == Error && r.Body != nil {
			t.Errorf("%s: erro com corpo", tc.target)
		}
		if (tc.want == Complete || tc.want == Truncated) && len(r.Body) == 0 {
			t.Errorf("%s: sem corpo", tc.target)
		}
	}
	if !strings.HasPrefix(f.headers.Get("Accept"), "application/rdap+json") {
		t.Errorf("Accept = %q", f.headers.Get("Accept"))
	}
	if f.headers.Get("User-Agent") != "badblock-collector-registrobr-rdapnum/test" {
		t.Errorf("User-Agent = %q", f.headers.Get("User-Agent"))
	}
}

func TestTruncatedOnlyByRemark(t *testing.T) {
	// Sem e-mail nenhum, mas sem a marca: completa.
	body := []byte(`{"objectClassName":"autnum","entities":[{"handle":"X","vcardArray":["vcard",[["fn",{},"text","A"]]]}]}`)
	if r, err := Classify(200, body, "autnum"); r != Complete || err != nil {
		t.Errorf("sem marca = %v %v", r, err)
	}
	// A marca em caixa diferente ainda é a marca.
	body = []byte(`{"objectClassName":"autnum","remarks":[{"type":"Object Truncated Due To Server Policy"}]}`)
	if r, _ := Classify(200, body, "autnum"); r != Truncated {
		t.Errorf("caixa = %v", r)
	}
	// Outro remarks não trunca.
	body = []byte(`{"objectClassName":"autnum","remarks":[{"type":"object redacted"}]}`)
	if r, _ := Classify(200, body, "autnum"); r != Complete {
		t.Errorf("outro remarks = %v", r)
	}
}

func TestNotice(t *testing.T) {
	c, _ := newClient(t, time.Second)
	r := c.Fetch(context.Background(), KindASN, "61610")
	if r.EgressIP != netip.MustParseAddr("192.0.2.1") {
		t.Errorf("egress = %v", r.EgressIP)
	}
	want := time.Date(2026, 10, 21, 20, 36, 1, 0, time.UTC)
	if !r.ServerTime.Equal(want) {
		t.Errorf("server_time = %v", r.ServerTime)
	}
	// Sem a segunda linha: as duas vazias, sem recusar.
	_, ip, ok := ReadNotice(fixture(t, "synthetic-autnum-no-vcard.json"))
	if ok || ip.IsValid() {
		t.Errorf("aviso ausente lido: %v %v", ip, ok)
	}
	_, _, ok = ReadNotice([]byte(`{"notices":[{"description":["x","ontem - não é ip"]}]}`))
	if ok {
		t.Error("linha diferente lida")
	}
}

// fakeProxy é um proxy HTTP de CONNECT que conta os túneis e exige senha.
type fakeProxy struct {
	connects atomic.Int64
	auth     string
}

func (p *fakeProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Proxy-Authorization") != "Basic "+p.auth {
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}
	p.connects.Add(1)
	dst, err := net.Dial("tcp", r.Host)
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	hj, _ := w.(http.Hijacker)
	conn, buf, err := hj.Hijack()
	if err != nil {
		dst.Close()
		return
	}
	_, _ = conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\n"))
	go func() { _, _ = io.Copy(dst, bufio.NewReader(buf)); dst.Close() }()
	go func() { _, _ = io.Copy(conn, dst); conn.Close() }()
}

func TestProxy(t *testing.T) {
	f := &fakeRDAP{t: t}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	tlsCfg := srv.Client().Transport.(*http.Transport).TLSClientConfig.Clone()

	p := &fakeProxy{auth: base64.StdEncoding.EncodeToString([]byte("user:segredo"))}
	psrv := httptest.NewServer(p)
	t.Cleanup(psrv.Close)
	pu, _ := url.Parse(psrv.URL)

	good := *pu
	good.User = url.UserPassword("user", "segredo")
	c := New(Options{BaseURL: srv.URL, Proxy: &good, Timeout: 2 * time.Second, UserAgent: "ua", TLS: tlsCfg})
	r := c.Fetch(context.Background(), KindASN, "61610")
	if r.Result != Complete {
		t.Fatalf("pelo proxy = %v %v", r.Result, r.Err)
	}
	if n := p.connects.Load(); n != 1 {
		t.Errorf("CONNECT = %d, quero 1", n)
	}

	bad := *pu
	bad.User = url.UserPassword("user", "errada")
	c = New(Options{BaseURL: srv.URL, Proxy: &bad, Timeout: 2 * time.Second, UserAgent: "ua", TLS: tlsCfg})
	r = c.Fetch(context.Background(), KindASN, "61610")
	if r.Result != Error || !r.ProxyErr {
		t.Errorf("senha errada = %v proxy=%v %v", r.Result, r.ProxyErr, r.Err)
	}
	if strings.Contains(r.Err.Error(), "errada") {
		t.Errorf("o erro mostra a senha: %v", r.Err)
	}

	// Proxy fora do ar.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	down := &url.URL{Scheme: "http", Host: addr}
	c = New(Options{BaseURL: srv.URL, Proxy: down, Timeout: 2 * time.Second, UserAgent: "ua", TLS: tlsCfg})
	r = c.Fetch(context.Background(), KindASN, "61610")
	if r.Result != Error || !r.ProxyErr || r.HTTPStatus != 0 {
		t.Errorf("proxy fora = %v proxy=%v status=%d %v", r.Result, r.ProxyErr, r.HTTPStatus, r.Err)
	}

	// Sem proxy, o mesmo erro de rede não é do proxy.
	c = New(Options{BaseURL: "https://" + addr, Timeout: time.Second, UserAgent: "ua", TLS: &tls.Config{}})
	r = c.Fetch(context.Background(), KindASN, "61610")
	if r.Result != Error || r.ProxyErr {
		t.Errorf("sem proxy = %v proxy=%v", r.Result, r.ProxyErr)
	}
}

func TestURL(t *testing.T) {
	if u := URL("https://rdap.registro.br", KindASN, "61610"); u != "https://rdap.registro.br/autnum/61610" {
		t.Error(u)
	}
	if u := URL("https://rdap.registro.br", KindPrefix, "2804:8ae0::/32"); u != "https://rdap.registro.br/ip/2804:8ae0::/32" {
		t.Error(u)
	}
}
