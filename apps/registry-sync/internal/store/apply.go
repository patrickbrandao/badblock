package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Table descreve uma tabela de ingest para a carga genérica. Toda tabela tem a
// coluna source_id, que não entra em Key nem em Columns.
type Table struct {
	Name    string   // nome qualificado, ex.: ingest.delegation
	Key     []string // chave natural dentro da fonte
	Columns []string // demais colunas
}

// Tabelas de ingest. As colunas seguem a ordem das linhas montadas em
// internal/syncer/convert.go.
var (
	TableDelegation = Table{"ingest.delegation", []string{"rtype", "start"},
		[]string{"rir", "value", "cc", "reg_date", "status", "opaque_id", "asn_first", "asn_last", "cidrs"}}
	TableIANAASNBlock = Table{"ingest.iana_asn_block", []string{"asn_first"},
		[]string{"asn_last", "description", "whois", "rdap", "reference", "reg_date"}}
	TableIANAIPBlock = Table{"ingest.iana_ip_block", []string{"prefix"},
		[]string{"designation", "reg_date", "whois", "rdap", "status", "note"}}
	TableIANASpecialIP = Table{"ingest.iana_special_ip", []string{"prefix"},
		[]string{"name", "rfc", "alloc_date", "termination", "source_ok", "destination_ok",
			"forwardable", "globally_reachable", "reserved_by_protocol"}}
	TableIANASpecialASN = Table{"ingest.iana_special_asn", []string{"asn_first"},
		[]string{"asn_last", "reason", "reference"}}
	TableRDAPService = Table{"ingest.rdap_service", []string{"entry"},
		[]string{"asn_first", "asn_last", "prefix", "base_url"}}
	TableNICBRASN    = Table{"ingest.nicbr_asn", []string{"asn"}, []string{"name", "document"}}
	TableNICBRPrefix = Table{"ingest.nicbr_prefix", []string{"asn", "prefix"}, nil}
	TableASName      = Table{"ingest.asname", []string{"asn"}, []string{"handle", "name", "cc"}}
)

// TableData são as linhas novas de uma tabela, na ordem Key + Columns.
type TableData struct {
	Table Table
	Rows  [][]any
}

// ApplyInput é uma carga completa de uma fonte.
type ApplyInput struct {
	SourceID         string
	Tables           []TableData
	RemovalThreshold float64 // fração máxima de linhas da fonte que pode sumir
	Force            bool    // ignora a trava de remoção
	Run              Run     // completado com as contagens e gravado no fim
	ETag             string
	LastModified     string
}

// ApplyStats resume o efeito de uma carga.
type ApplyStats struct {
	Inserted, Updated, Deleted int64
}

// ErrRemovalThreshold indica que a fonte removeria mais linhas do que o
// permitido. A carga é abortada e nada muda no banco.
var ErrRemovalThreshold = errors.New("trava de remoção")

// Apply substitui as linhas da fonte em cada tabela, numa transação só, e
// atualiza o estado da fonte. Para cada tabela: COPY numa staging temporária,
// conferência de chave duplicada, trava de remoção e MERGE.
func (s *Store) Apply(ctx context.Context, in ApplyInput) (ApplyStats, error) {
	var total ApplyStats
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return total, err
	}
	defer tx.Rollback(ctx)

	for i, td := range in.Tables {
		st, err := applyTable(ctx, tx, i, in.SourceID, td, in.RemovalThreshold, in.Force)
		if err != nil {
			return total, fmt.Errorf("%s: %w", td.Table.Name, err)
		}
		total.Inserted += st.Inserted
		total.Updated += st.Updated
		total.Deleted += st.Deleted
	}

	r := in.Run
	r.Inserted, r.Updated, r.Deleted = total.Inserted, total.Updated, total.Deleted
	_, err = tx.Exec(ctx, `
		INSERT INTO ingest.source_state
		       (source_id, url, etag, last_modified, content_sha256, file_date, records,
		        last_checked_at, last_changed_at, last_success_at, last_error, last_error_at, updated_at)
		VALUES ($1::text, $2::text, nullif($3::text, ''), nullif($4::text, ''), $5::text, $6::date,
		        $7::bigint, $8::timestamptz, $8::timestamptz, $8::timestamptz, NULL, NULL, now())
		ON CONFLICT (source_id) DO UPDATE SET
		       url = excluded.url, etag = excluded.etag, last_modified = excluded.last_modified,
		       content_sha256 = excluded.content_sha256, file_date = excluded.file_date,
		       records = excluded.records, last_checked_at = excluded.last_checked_at,
		       last_changed_at = excluded.last_changed_at, last_success_at = excluded.last_success_at,
		       last_error = NULL, updated_at = now()`,
		in.SourceID, r.URL, in.ETag, in.LastModified, r.SHA256, r.FileDate, r.Records, r.FinishedAt)
	if err != nil {
		return total, fmt.Errorf("source_state: %w", err)
	}
	if err := insertRun(ctx, tx, r); err != nil {
		return total, fmt.Errorf("source_run: %w", err)
	}
	return total, tx.Commit(ctx)
}

func applyTable(ctx context.Context, tx pgx.Tx, idx int, sourceID string, td TableData, threshold float64, force bool) (ApplyStats, error) {
	var st ApplyStats
	t := td.Table
	stage := fmt.Sprintf("stage_%d", idx)
	cols := append(append([]string{"source_id"}, t.Key...), t.Columns...)

	// A staging copia a estrutura da tabela real e some no fim da transação.
	if _, err := tx.Exec(ctx, fmt.Sprintf(
		`CREATE TEMP TABLE %s (LIKE %s INCLUDING DEFAULTS) ON COMMIT DROP`, stage, t.Name)); err != nil {
		return st, err
	}
	rows := make([][]any, len(td.Rows))
	for i, r := range td.Rows {
		if len(r) != len(t.Key)+len(t.Columns) {
			return st, fmt.Errorf("linha %d com %d valores, esperados %d", i, len(r), len(t.Key)+len(t.Columns))
		}
		rows[i] = append([]any{sourceID}, r...)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{stage}, cols, pgx.CopyFromRows(rows)); err != nil {
		return st, fmt.Errorf("copy: %w", err)
	}

	keyList := strings.Join(t.Key, ", ")
	var dups int64
	if err := tx.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM (SELECT 1 FROM %s GROUP BY %s HAVING count(*) > 1) d`, stage, keyList)).
		Scan(&dups); err != nil {
		return st, err
	}
	if dups > 0 {
		return st, fmt.Errorf("%d chaves repetidas na fonte", dups)
	}

	join := joinCond("t", "s", t.Key)
	var existing, missing int64
	if err := tx.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*),
		       count(*) FILTER (WHERE NOT EXISTS (SELECT 1 FROM %s s WHERE %s))
		  FROM %s t
		 WHERE t.source_id = $1`, stage, join, t.Name), sourceID).Scan(&existing, &missing); err != nil {
		return st, err
	}
	if existing > 0 && !force && float64(missing)/float64(existing) > threshold {
		return st, fmt.Errorf("%w: a carga removeria %d de %d linhas (%.1f%%, limite %.1f%%); use --force se for legítimo",
			ErrRemovalThreshold, missing, existing, 100*float64(missing)/float64(existing), 100*threshold)
	}

	if err := tx.QueryRow(ctx, mergeSQL(t, stage), sourceID).Scan(&st.Inserted, &st.Updated, &st.Deleted); err != nil {
		return st, fmt.Errorf("merge: %w", err)
	}
	return st, nil
}

// mergeSQL monta o MERGE que deixa as linhas da fonte iguais às da staging:
// insere as novas, atualiza as que mudaram e apaga as que sumiram. O RETURNING
// merge_action() dentro do WITH (PG 17+) permite contar cada tipo de ação.
func mergeSQL(t Table, stage string) string {
	all := append(append([]string{"source_id"}, t.Key...), t.Columns...)
	var b strings.Builder
	fmt.Fprintf(&b, "WITH m AS (\n  MERGE INTO %s AS t\n  USING %s AS s\n  ON t.source_id = s.source_id AND %s\n",
		t.Name, stage, joinCond("t", "s", t.Key))
	if len(t.Columns) > 0 {
		var sets, tCols, sCols []string
		for _, c := range t.Columns {
			sets = append(sets, fmt.Sprintf("%s = s.%s", c, c))
			tCols = append(tCols, "t."+c)
			sCols = append(sCols, "s."+c)
		}
		// ROW(...) mantém a comparação válida mesmo com uma coluna só.
		fmt.Fprintf(&b, "  WHEN MATCHED AND ROW(%s) IS DISTINCT FROM ROW(%s) THEN\n    UPDATE SET %s\n",
			strings.Join(tCols, ", "), strings.Join(sCols, ", "), strings.Join(sets, ", "))
	}
	var sVals []string
	for _, c := range all {
		sVals = append(sVals, "s."+c)
	}
	fmt.Fprintf(&b, "  WHEN NOT MATCHED THEN\n    INSERT (%s) VALUES (%s)\n", strings.Join(all, ", "), strings.Join(sVals, ", "))
	b.WriteString("  WHEN NOT MATCHED BY SOURCE AND t.source_id = $1 THEN\n    DELETE\n")
	b.WriteString("  RETURNING merge_action() AS action\n)\n")
	b.WriteString("SELECT count(*) FILTER (WHERE action = 'INSERT'),\n" +
		"       count(*) FILTER (WHERE action = 'UPDATE'),\n" +
		"       count(*) FILTER (WHERE action = 'DELETE')\n  FROM m")
	return b.String()
}

func joinCond(a, b string, cols []string) string {
	var parts []string
	for _, c := range cols {
		parts = append(parts, fmt.Sprintf("%s.%s = %s.%s", a, c, b, c))
	}
	return strings.Join(parts, " AND ")
}

// Now é o relógio usado nas execuções (substituível nos testes).
var Now = func() time.Time { return time.Now().UTC() }
