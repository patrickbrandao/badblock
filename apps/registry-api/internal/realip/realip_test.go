package realip

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	r, err := New(DefaultTrustedProxies, DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		remote  string
		xff     []string
		realIP  string
		want    string
		wantSrc Source
	}{
		{"cliente direto, sem headers", "200.1.2.3:5555", nil, "", "200.1.2.3", SourcePeer},
		{"cliente direto forjando XFF", "200.1.2.3:5555", []string{"8.8.8.8"}, "", "200.1.2.3", SourcePeer},
		{"cliente direto forjando X-Real-IP", "200.1.2.3:5555", nil, "8.8.8.8", "200.1.2.3", SourcePeer},
		{"via Traefik", "172.18.0.2:40000", []string{"200.1.2.3"}, "200.1.2.3", "200.1.2.3", SourceForwardedFor},
		{"via Traefik, cliente forjou antes", "172.18.0.2:40000", []string{"8.8.8.8, 200.1.2.3"}, "", "200.1.2.3", SourceForwardedFor},
		{"dois proxies internos", "172.18.0.2:40000", []string{"200.1.2.3, 10.0.0.5"}, "", "200.1.2.3", SourceForwardedFor},
		{"várias linhas de XFF", "172.18.0.2:40000", []string{"9.9.9.9", "200.1.2.3"}, "", "200.1.2.3", SourceForwardedFor},
		{"só proxies internos", "172.18.0.2:40000", []string{"10.0.0.9, 10.0.0.5"}, "", "10.0.0.9", SourceForwardedFor},
		{"XFF inválido cai no X-Real-IP", "172.18.0.2:40000", []string{"lixo"}, "200.1.2.3", "200.1.2.3", SourceRealIP},
		{"entrada inválida no meio", "172.18.0.2:40000", []string{"8.8.8.8, lixo, 10.0.0.5"}, "", "172.18.0.2", SourcePeer},
		{"XFF com porta", "172.18.0.2:40000", []string{"200.1.2.3:1234"}, "", "200.1.2.3", SourceForwardedFor},
		{"IPv6 entre colchetes", "[fd00::2]:40000", []string{"[2804:5964::1]:443"}, "", "2804:5964::1", SourceForwardedFor},
		{"IPv6 simples", "[::1]:40000", []string{"2804:5964::1"}, "", "2804:5964::1", SourceForwardedFor},
		{"IPv4 mapeado", "[::ffff:172.18.0.2]:40000", []string{"::ffff:200.1.2.3"}, "", "200.1.2.3", SourceForwardedFor},
		{"proxy confiável sem headers", "172.18.0.2:40000", nil, "", "172.18.0.2", SourcePeer},
		{"zona IPv6 rejeitada", "172.18.0.2:40000", []string{"fe80::1%eth0"}, "", "172.18.0.2", SourcePeer},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		for _, v := range c.xff {
			req.Header.Add("X-Forwarded-For", v)
		}
		if c.realIP != "" {
			req.Header.Set("X-Real-IP", c.realIP)
		}
		got, src := r.ClientIP(req)
		if got.String() != c.want || src != c.wantSrc {
			t.Errorf("%s: = %s (%s), esperado %s (%s)", c.name, got, src, c.want, c.wantSrc)
		}
	}
}

func TestHeaderOrder(t *testing.T) {
	r, err := New(DefaultTrustedProxies, "X-Real-IP,X-Forwarded-For")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.18.0.2:1"
	req.Header.Set("X-Forwarded-For", "200.1.2.3")
	req.Header.Set("X-Real-IP", "200.9.9.9")
	if got, src := r.ClientIP(req); got.String() != "200.9.9.9" || src != SourceRealIP {
		t.Errorf("com X-Real-IP primeiro = %s (%s)", got, src)
	}
}

func TestNewErrors(t *testing.T) {
	if _, err := New("10.0.0.0/33", DefaultHeaders); err == nil {
		t.Error("faixa inválida deveria falhar")
	}
	if _, err := New(DefaultTrustedProxies, "CF-Connecting-IP"); err == nil {
		t.Error("header não suportado deveria falhar")
	}
	r, err := New("10.1.2.3", DefaultHeaders)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.1.2.3:1"
	req.Header.Set("X-Forwarded-For", "200.1.2.3")
	if got, _ := r.ClientIP(req); got.String() != "200.1.2.3" {
		t.Errorf("IP isolado na lista deveria virar /32: %s", got)
	}
}
