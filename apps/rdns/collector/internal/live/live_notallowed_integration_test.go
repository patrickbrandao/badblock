//go:build integration

package live

import (
	"context"
	"log/slog"
	"net/netip"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/limiter"
	"github.com/patrickbrandao/badblock/apps/rdns/collector/internal/resolver"
)

// deletingLookuper apaga o prefixo do IP durante a consulta: o prefixo some
// depois do LiveAllowed e antes do SaveLive (tarefa 013).
type deletingLookuper struct{ admin *pgxpool.Pool }

func (d deletingLookuper) Lookup(ctx context.Context, ip netip.Addr, _ limiter.Priority, _ *limiter.Ticket) (resolver.Result, error) {
	if _, err := d.admin.Exec(ctx, "DELETE FROM rdns_prefix WHERE prefix >>= $1::inet", ip.String()); err != nil {
		return resolver.Result{}, err
	}
	return resolver.Result{IP: ip, Status: resolver.PTR, Names: []string{"registro.br"}}, nil
}

// TestLivePrefixoApagadoDuranteConsulta: o pedido passa no LiveAllowed, o
// prefixo é apagado durante a consulta e o SaveLive recusa. Quem pediu recebe
// not_allowed (e não o dado frio) e rdns_ptr não ganha linha.
func TestLivePrefixoApagadoDuranteConsulta(t *testing.T) {
	st, admin := setupPostgres(t)
	rdb := setupValkey(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := admin.Exec(ctx, "INSERT INTO rdns_prefix (prefix, asn) VALUES ('200.160.2.0/24', 22548)"); err != nil {
		t.Fatal(err)
	}
	loop := New(rdb, st, deletingLookuper{admin: admin}, "01a1243c-6b0e-7c5d-9a51-3f0f6c2d1e77", slog.New(slog.DiscardHandler))
	done := make(chan struct{})
	go func() { loop.Run(ctx); close(done) }()

	r := request(t, rdb, "200.160.2.3", time.Now())
	if r == nil || r.Result != "not_allowed" || len(r.Names) != 0 {
		t.Fatalf("resposta = %+v, quero not_allowed sem nomes", r)
	}
	var n int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM rdns_ptr").Scan(&n); err != nil || n != 0 {
		t.Errorf("linhas em rdns_ptr = %d (%v), quero 0", n, err)
	}
	if rdb.Exists(ctx, InflightPrefix+"200.160.2.3").Val() != 0 {
		t.Error("a marca inflight deveria ser apagada")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("o laço não parou com o encerramento")
	}
}
