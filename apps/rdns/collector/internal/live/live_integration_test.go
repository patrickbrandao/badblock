//go:build integration

package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/miekg/dns"
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/store"
)

// setupPostgres sobe um PG18 com as migrations de central/, cgibr/ e rdns/.
func setupPostgres(t *testing.T) (*store.Store, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pg, err := postgres.Run(ctx, "postgres:18-trixie",
		postgres.WithDatabase("badblock"), postgres.WithUsername("postgres"), postgres.WithPassword("pg"),
		postgres.BasicWaitStrategies())
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
	t.Cleanup(admin.Close)
	for _, dir := range []string{"central", "cgibr", "rdns"} {
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
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st, admin
}

// setupValkey sobe um Valkey descartável (container genérico).
func setupValkey(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	c, err := testcontainers.Run(ctx, "valkey/valkey:9-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	ep, err := c.PortEndpoint(ctx, "6379/tcp", "")
	if err != nil {
		t.Fatal(err)
	}
	rdb, err := NewClient("redis://" + ep + "/0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

// startDNS sobe um resolvedor falso que responde registro.br. a toda pergunta.
func startDNS(t *testing.T) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, req *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(req)
		rr, _ := dns.NewRR(req.Question[0].Name + " 60 IN PTR registro.br.")
		m.Answer = append(m.Answer, rr)
		_ = w.WriteMsg(m)
	})}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

// request faz o papel da api-rdns: marca, enfileira e espera a resposta.
func request(t *testing.T, rdb *redis.Client, ip string, at time.Time) *Reply {
	t.Helper()
	ctx := context.Background()
	if err := rdb.SetNX(ctx, InflightPrefix+ip, "1", 30*time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(Request{IP: ip, At: at})
	if err := rdb.RPush(ctx, QueueKey, body).Err(); err != nil {
		t.Fatal(err)
	}
	// A resposta chega com expiração (antes do BLPOP, para ver o TTL).
	key := ReplyPrefix + ip
	deadline := time.Now().Add(5 * time.Second)
	for rdb.Exists(ctx, key).Val() == 0 {
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	if ttl := rdb.TTL(ctx, key).Val(); ttl <= 0 || ttl > ReplyTTL {
		t.Errorf("TTL da lista de retorno = %v", ttl)
	}
	vals, err := rdb.BLPop(ctx, time.Second, key).Result()
	if err != nil {
		t.Fatal(err)
	}
	var r Reply
	if err := json.Unmarshal([]byte(vals[1]), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func TestLiveEndToEnd(t *testing.T) {
	st, admin := setupPostgres(t)
	rdb := setupValkey(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := admin.Exec(ctx, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548), ('200.160.3.0/24', 22548)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "UPDATE rdns_prefix SET active = false WHERE prefix = '200.160.3.0/24'"); err != nil {
		t.Fatal(err)
	}
	lim := limiter.New(1000)
	go lim.Run(ctx)
	res := resolver.New(resolver.Options{Servers: []string{startDNS(t)}, Timeout: time.Second, Retries: 1,
		Inflight: 8, Limiter: lim})
	loop := New(rdb, st, res, "01a1243c-6b0e-7c5d-9a51-3f0f6c2d1e77", slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()

	// Pedido → linha em rdns_ptr → resposta com expiração → marca apagada.
	r := request(t, rdb, "200.160.2.3", time.Now())
	if r == nil || r.Result != "ptr" || len(r.Names) != 1 || r.Names[0] != "registro.br" ||
		r.Worker != "01a1243c-6b0e-7c5d-9a51-3f0f6c2d1e77" || r.CheckedAt == "" {
		t.Fatalf("resposta = %+v", r)
	}
	var name string
	if err := admin.QueryRow(ctx, "SELECT name FROM rdns_ptr WHERE ip = '200.160.2.3'").Scan(&name); err != nil || name != "registro.br" {
		t.Errorf("rdns_ptr = %q, %v", name, err)
	}
	if rdb.Exists(ctx, InflightPrefix+"200.160.2.3").Val() != 0 {
		t.Error("a marca inflight deveria ser apagada")
	}
	var lastScan *time.Time
	_ = admin.QueryRow(ctx, "SELECT last_scan_at FROM rdns_prefix WHERE prefix = '200.160.2.0/24'").Scan(&lastScan)
	if lastScan != nil {
		t.Error("o tempo real não muda o last_scan_at do prefixo")
	}

	// Fora de prefixo ativo: not_allowed, sem consulta nem gravação.
	before := res.Queries()
	for _, ip := range []string{"8.8.8.8", "200.160.3.1"} {
		r = request(t, rdb, ip, time.Now())
		if r == nil || r.Result != "not_allowed" || r.Names == nil || len(r.Names) != 0 {
			t.Errorf("%s = %+v", ip, r)
		}
	}
	if res.Queries() != before {
		t.Error("not_allowed não deveria consultar o DNS")
	}
	var n int
	_ = admin.QueryRow(ctx, "SELECT count(*) FROM rdns_ptr").Scan(&n)
	if n != 1 {
		t.Errorf("linhas em rdns_ptr = %d", n)
	}

	// Pedido vencido: descartado, marca apagada, sem resposta.
	if r = request(t, rdb, "200.160.2.4", time.Now().Add(-time.Minute)); r != nil {
		t.Errorf("pedido vencido respondido: %+v", r)
	}
	if rdb.Exists(ctx, InflightPrefix+"200.160.2.4").Val() != 0 {
		t.Error("pedido vencido: a marca inflight deveria ser apagada")
	}
	// Mensagem ilegível: descartada, o laço segue.
	_ = rdb.RPush(ctx, QueueKey, "{nada").Err()
	if r = request(t, rdb, "200.160.2.5", time.Now()); r == nil || r.Result != "ptr" {
		t.Errorf("depois da mensagem ilegível = %+v", r)
	}
	if loop.Served() != 4 {
		t.Errorf("Served = %d, quero 4", loop.Served())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("o laço não parou com o encerramento")
	}
}
