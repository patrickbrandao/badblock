//go:build integration

package scan

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/config"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/store"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/worker"
)

// TestRealNet coleta um ASN e os blocos dele no RDAP de verdade, a 8/min,
// num PG18 descartável, e mostra o resultado e o IP de saída. Com BURST=N,
// faz depois N consultas seguidas ao mesmo ASN, sem o limitador, e mostra a
// partir de qual veio truncada (o ensaio de fonte.md#limite-do-servidor).
// Nunca roda sozinho: make test-real ASN=61610 [BURST=30] (variáveis
// RDAPNUM_REAL_ASN e RDAPNUM_REAL_BURST; PROXY_URL, se definida, vale).
func TestRealNet(t *testing.T) {
	asnEnv := os.Getenv("RDAPNUM_REAL_ASN")
	if asnEnv == "" {
		t.Skip("defina RDAPNUM_REAL_ASN (make test-real ASN=...)")
	}
	asn, err := strconv.ParseUint(asnEnv, 10, 32)
	if err != nil {
		t.Fatalf("RDAPNUM_REAL_ASN inválido: %q", asnEnv)
	}
	burst, _ := strconv.Atoi(os.Getenv("RDAPNUM_REAL_BURST"))
	proxy, err := config.ParseProxyURL(os.Getenv("PROXY_URL"))
	if err != nil {
		t.Fatal(err)
	}
	const rate = config.SafeRateWithoutProxy

	url, admin := setupPostgres(t, "central", "registrobr_rdapnum")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := admin.Exec(ctx, "INSERT INTO registrobr_rdapnum_target (kind, asn) VALUES ('asn', $1)", asn); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id := worker.NewUUID()
	if err := st.Heartbeat(ctx, store.Worker{UUID: id, Host: "test-real", Version: "test", RateLimit: rate, Proxy: proxy != nil}); err != nil {
		t.Fatal(err)
	}
	lim := limiter.New(rate)
	client := rdap.New(rdap.Options{BaseURL: "https://rdap.registro.br", Proxy: proxy, Timeout: 20 * time.Second,
		UserAgent: config.UserAgent("test-real")})
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	sc := New(st, client, lim, Options{Worker: id, Lease: 2 * time.Minute, Interval: func() time.Duration { return 168 * time.Hour },
		Proxy: proxy != nil, Idle: time.Second}, log)
	done := make(chan struct{})
	go func() { sc.Run(ctx); close(done) }()

	wait := func(what string) {
		deadline := time.Now().Add(3 * time.Minute)
		for {
			var pending int
			_ = admin.QueryRow(ctx, "SELECT count(*) FROM registrobr_rdapnum_target WHERE last_attempt_at IS NULL").Scan(&pending)
			if pending == 0 {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: faltam %d alvos", what, pending)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	start := time.Now()
	wait("ASN")

	// Os blocos que a resposta do ASN lista em links (rel related).
	var body []byte
	_ = admin.QueryRow(ctx, `SELECT r.body::text FROM registrobr_rdapnum_target t
		JOIN registrobr_rdapnum_response r ON r.target_uuid = t.uuid WHERE t.asn = $1 ORDER BY r.fetched_at DESC LIMIT 1`, asn).Scan(&body)
	var obj struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	_ = json.Unmarshal(body, &obj)
	for _, l := range obj.Links {
		if _, p, ok := strings.Cut(l.Href, "/ip/"); ok && l.Rel == "related" {
			if _, err := admin.Exec(ctx, "INSERT INTO registrobr_rdapnum_target (kind, prefix) VALUES ('prefix', $1::cidr) ON CONFLICT DO NOTHING", p); err != nil {
				t.Logf("bloco %s: %v", p, err)
			}
		}
	}
	wait("blocos")
	cancel()
	<-done

	rows, err := admin.Query(context.Background(), `
		SELECT coalesce(t.asn::text, t.prefix::text), t.last_result, t.last_http_status, coalesce(r.bytes, 0),
		       coalesce(host(r.egress_ip), '-'), coalesce(r.process_error, '')
		  FROM registrobr_rdapnum_target t
		  LEFT JOIN LATERAL (SELECT * FROM registrobr_rdapnum_response WHERE target_uuid = t.uuid
		                      ORDER BY fetched_at DESC LIMIT 1) r ON true
		 ORDER BY t.kind, t.asn, t.prefix`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var target, egress, perr string
		var result, status *int16
		var bytes int
		_ = rows.Scan(&target, &result, &status, &bytes, &egress, &perr)
		t.Logf("%-20s result=%s http=%v bytes=%d egress=%s %s", target, rdap.Result(deref(result)), deref(status), bytes, egress, perr)
	}
	rows.Close()
	var autnum, network, entity, reverse int
	_ = admin.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM registrobr_rdapnum_autnum), (SELECT count(*) FROM registrobr_rdapnum_network),
		(SELECT count(*) FROM registrobr_rdapnum_entity), (SELECT count(*) FROM registrobr_rdapnum_reverse)`).Scan(&autnum, &network, &entity, &reverse)
	var stats store.Worker
	sc.Stats(&stats)
	t.Logf("em %s a %d/min: %d consultas (%d completas, %d truncadas, %d 404, %d erros); processadas: %d ASN, %d blocos, %d entidades, %d reversos",
		time.Since(start).Round(time.Second), rate, stats.Requests, stats.Complete, stats.Truncated, stats.NotFound, stats.Errors,
		autnum, network, entity, reverse)

	if burst <= 0 {
		return
	}
	var marks []string
	first := 0
	for i := 1; i <= burst; i++ {
		r := client.Fetch(context.Background(), rdap.KindASN, asnEnv)
		marks = append(marks, fmt.Sprintf("%d:%s", i, r.Result))
		if r.Result == rdap.Truncated && first == 0 {
			first = i
		}
	}
	t.Logf("rajada de %d: primeira truncada = %d; %s", burst, first, strings.Join(marks, " "))
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
