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
		name, remote, xff, xri, want, source string
	}{
		{"direto, ignora headers", "203.0.113.9:1234", "1.1.1.1", "2.2.2.2", "203.0.113.9", "peer"},
		{"via Traefik", "10.249.255.253:1234", "198.51.100.7", "", "198.51.100.7", "x-forwarded-for"},
		{"xff forjado à esquerda", "10.0.0.2:1", "6.6.6.6, 198.51.100.7, 10.0.0.9", "", "198.51.100.7", "x-forwarded-for"},
		{"x-real-ip de reserva", "10.0.0.2:1", "", "2001:db8::1", "2001:db8::1", "x-real-ip"},
		{"xff inválido", "10.0.0.2:1", "lixo", "", "10.0.0.2", "peer"},
		{"ipv4 mapeado", "[::ffff:10.0.0.2]:1", "198.51.100.7", "", "198.51.100.7", "x-forwarded-for"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		if c.xff != "" {
			req.Header.Set("X-Forwarded-For", c.xff)
		}
		if c.xri != "" {
			req.Header.Set("X-Real-IP", c.xri)
		}
		ip, src := r.ClientIP(req)
		if ip.String() != c.want || src != c.source {
			t.Errorf("%s: %s (%s), quero %s (%s)", c.name, ip, src, c.want, c.source)
		}
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	if _, err := New("10.0.0.0/33", DefaultHeaders); err == nil {
		t.Error("faixa inválida deveria falhar")
	}
	if _, err := New(DefaultTrustedProxies, "Forwarded"); err == nil {
		t.Error("header não suportado deveria falhar")
	}
}
