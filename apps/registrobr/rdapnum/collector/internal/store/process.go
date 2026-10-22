package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/parse"
	"github.com/patrickbrandao/badblock/apps/registrobr/rdapnum/collector/internal/rdap"
)

// Processed é o que o processamento de uma resposta fez.
type Processed struct {
	Skipped bool   // outra instância está processando a mesma, ou ela sumiu
	Stale   bool   // não é mais a resposta atual do alvo: marcada e nada gravado
	Kind    string // autnum, ip network ou not_found
	Changed bool   // alguma tabela processada mudou
	Refused error  // recusada pelo parser (gravado em process_error)
}

// Process processa uma resposta numa transação
// (dados-processados.md#mapeamento-resposta--colunas): só a resposta atual
// do alvo, só completas e 404. Uma truncada nunca chega aqui; se chegar, é
// erro. Recusa do parser: process_error, e o dado anterior continua.
func (s *Store) Process(ctx context.Context, responseUUID string) (Processed, error) {
	var out Processed
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)

	var target, kind string
	var current *string
	var result int16
	var body []byte
	err = tx.QueryRow(ctx, `
		SELECT r.target_uuid::text, r.result, r.body::text, t.response_uuid::text, t.kind
		  FROM registrobr_rdapnum_response r
		  JOIN registrobr_rdapnum_target t ON t.uuid = r.target_uuid
		 WHERE r.uuid = $1::uuid
		   FOR UPDATE OF r SKIP LOCKED`, responseUUID).Scan(&target, &result, &body, &current, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		out.Skipped = true
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("registrobr_rdapnum_response: %w", err)
	}
	if current == nil || *current != responseUUID {
		out.Stale = true
		if _, err := tx.Exec(ctx, `UPDATE registrobr_rdapnum_response SET processed_at = NOW() WHERE uuid = $1::uuid`,
			responseUUID); err != nil {
			return out, err
		}
		return out, tx.Commit(ctx)
	}

	switch rdap.Result(result) {
	case rdap.NotFound:
		out.Kind = "not_found"
		n, err := deleteObjects(ctx, tx, target)
		if err != nil {
			return out, err
		}
		out.Changed = n > 0
	case rdap.Complete:
		p, perr := parse.Parse(body)
		if perr == nil && p.Class != rdap.ExpectedClass(kind) {
			perr = fmt.Errorf("%w: objectClassName %q não é de um alvo %s", parse.ErrRefused, p.Class, kind)
		}
		if perr != nil {
			out.Refused = perr
			if _, err := tx.Exec(ctx, `
				UPDATE registrobr_rdapnum_response SET parser_version = $2, processed_at = NOW(), process_error = $3
				 WHERE uuid = $1::uuid`, responseUUID, parse.Version, parse.Reason(perr)); err != nil {
				return out, err
			}
			return out, tx.Commit(ctx)
		}
		out.Kind = p.Class
		if out.Changed, err = writeParsed(ctx, tx, target, responseUUID, p); err != nil {
			return out, err
		}
	default:
		return out, fmt.Errorf("resposta %s com result %d não se processa", responseUUID, result)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE registrobr_rdapnum_response SET parser_version = $2, processed_at = NOW(), process_error = NULL
		 WHERE uuid = $1::uuid`, responseUUID, parse.Version); err != nil {
		return out, fmt.Errorf("registrobr_rdapnum_response: %w", err)
	}
	if out.Changed {
		if err := touchSync(ctx, tx); err != nil {
			return out, err
		}
	}
	return out, tx.Commit(ctx)
}

// RecordProcessError grava, fora da transação do processamento, a falha de
// uma resposta que não pôde ser processada (o reprocessamento tenta de novo
// depois de 1 h).
func (s *Store) RecordProcessError(ctx context.Context, responseUUID, msg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE registrobr_rdapnum_response SET parser_version = $2, processed_at = NOW(), process_error = $3
		 WHERE uuid = $1::uuid`, responseUUID, parse.Version, msg)
	return err
}

// deleteObjects apaga o ASN ou os blocos gerados por um alvo (404); as
// ligações e os reversos caem em cascata.
func deleteObjects(ctx context.Context, tx pgx.Tx, target string) (int64, error) {
	var n int64
	for _, table := range []string{"registrobr_rdapnum_autnum", "registrobr_rdapnum_network"} {
		tag, err := tx.Exec(ctx, "DELETE FROM "+table+" WHERE target_uuid = $1::uuid", target)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", table, err)
		}
		n += tag.RowsAffected()
	}
	return n, nil
}

// writeParsed grava uma resposta completa lida. Devolve se algo mudou.
func writeParsed(ctx context.Context, tx pgx.Tx, target, response string, p *parse.Parsed) (bool, error) {
	changed := false
	// Entidades em ordem de handle (p.Entities já vem assim): duas instâncias
	// que gravam as mesmas travam as linhas na mesma ordem.
	ids := map[string]string{}
	for _, e := range p.Entities {
		var id string
		var ch bool
		err := tx.QueryRow(ctx, `
			INSERT INTO registrobr_rdapnum_entity (handle, kind, name, email, country, document, document_type,
			       legal_representative, registered_at, changed_at, response_uuid)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid)
			ON CONFLICT (handle) DO UPDATE SET
				kind = EXCLUDED.kind, name = EXCLUDED.name, email = EXCLUDED.email, country = EXCLUDED.country,
				document = EXCLUDED.document, document_type = EXCLUDED.document_type,
				legal_representative = EXCLUDED.legal_representative,
				registered_at = EXCLUDED.registered_at, changed_at = EXCLUDED.changed_at,
				response_uuid = EXCLUDED.response_uuid
			RETURNING uuid::text, updated_at = NOW()`,
			e.Handle, e.Kind, e.Name, e.Email, e.Country, e.Document, e.DocumentType, e.LegalRepresentative,
			e.RegisteredAt, e.ChangedAt, response).Scan(&id, &ch)
		if err != nil {
			return false, fmt.Errorf("registrobr_rdapnum_entity: %w", err)
		}
		ids[e.Handle] = id
		changed = changed || ch
	}
	var registrant any
	if p.Registrant != "" {
		registrant = ids[p.Registrant]
	}

	var objCol, objID string
	if a := p.Autnum; a != nil {
		objCol = "autnum_uuid"
		tag, err := tx.Exec(ctx, `DELETE FROM registrobr_rdapnum_autnum WHERE target_uuid = $1::uuid AND asn <> $2`,
			target, a.ASN)
		if err != nil {
			return false, fmt.Errorf("registrobr_rdapnum_autnum: %w", err)
		}
		changed = changed || tag.RowsAffected() > 0
		var ch bool
		err = tx.QueryRow(ctx, `
			INSERT INTO registrobr_rdapnum_autnum (asn, target_uuid, response_uuid, handle, name, type, country,
			       registrant_uuid, registered_at, changed_at)
			VALUES ($1, $2::uuid, $3::uuid, $4, $5, $6, $7, $8::uuid, $9, $10)
			ON CONFLICT (asn) DO UPDATE SET
				target_uuid = EXCLUDED.target_uuid, response_uuid = EXCLUDED.response_uuid,
				handle = EXCLUDED.handle, name = EXCLUDED.name, type = EXCLUDED.type, country = EXCLUDED.country,
				registrant_uuid = EXCLUDED.registrant_uuid,
				registered_at = EXCLUDED.registered_at, changed_at = EXCLUDED.changed_at
			RETURNING uuid::text, updated_at = NOW()`,
			a.ASN, target, response, a.Handle, a.Name, a.Type, a.Country, registrant, a.RegisteredAt, a.ChangedAt).
			Scan(&objID, &ch)
		if err != nil {
			return false, fmt.Errorf("registrobr_rdapnum_autnum: %w", err)
		}
		changed = changed || ch
	} else {
		n := p.Network
		objCol = "network_uuid"
		// O registro.br reagrupou: o bloco antigo deste alvo sai.
		tag, err := tx.Exec(ctx, `DELETE FROM registrobr_rdapnum_network WHERE target_uuid = $1::uuid AND prefix <> $2::cidr`,
			target, n.Prefix.String())
		if err != nil {
			return false, fmt.Errorf("registrobr_rdapnum_network: %w", err)
		}
		changed = changed || tag.RowsAffected() > 0
		var ch bool
		err = tx.QueryRow(ctx, `
			INSERT INTO registrobr_rdapnum_network (prefix, target_uuid, response_uuid, handle, name, type, country,
			       parent_handle, status, asn, registrant_uuid, registered_at, changed_at)
			VALUES ($1::cidr, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11::uuid, $12, $13)
			ON CONFLICT (prefix) DO UPDATE SET
				target_uuid = EXCLUDED.target_uuid, response_uuid = EXCLUDED.response_uuid,
				handle = EXCLUDED.handle, name = EXCLUDED.name, type = EXCLUDED.type, country = EXCLUDED.country,
				parent_handle = EXCLUDED.parent_handle, status = EXCLUDED.status, asn = EXCLUDED.asn,
				registrant_uuid = EXCLUDED.registrant_uuid,
				registered_at = EXCLUDED.registered_at, changed_at = EXCLUDED.changed_at
			RETURNING uuid::text, updated_at = NOW()`,
			n.Prefix.String(), target, response, n.Handle, n.Name, n.Type, n.Country, n.ParentHandle, n.Status,
			n.ASN, registrant, n.RegisteredAt, n.ChangedAt).Scan(&objID, &ch)
		if err != nil {
			return false, fmt.Errorf("registrobr_rdapnum_network: %w", err)
		}
		changed = changed || ch
		ch, err = writeReverses(ctx, tx, objID, p.Reverses)
		if err != nil {
			return false, err
		}
		changed = changed || ch
	}
	ch, err := writeLinks(ctx, tx, objCol, objID, ids, p.Links)
	if err != nil {
		return false, err
	}
	return changed || ch, nil
}

// writeLinks regrava as ligações do objeto, se mudaram.
func writeLinks(ctx context.Context, tx pgx.Tx, col, obj string, ids map[string]string, links []parse.Link) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT e.handle || '|' || l.role || '|' || coalesce(v.handle, '')
		  FROM registrobr_rdapnum_link l
		  JOIN registrobr_rdapnum_entity e ON e.uuid = l.entity_uuid
		  LEFT JOIN registrobr_rdapnum_entity v ON v.uuid = l.via_uuid
		 WHERE l.`+col+` = $1::uuid`, obj)
	if err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_link: %w", err)
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_link: %w", err)
	}
	want := make([]string, 0, len(links))
	for _, l := range links {
		want = append(want, l.Handle+"|"+l.Role+"|"+l.Via)
	}
	slices.Sort(old)
	slices.Sort(want)
	if slices.Equal(old, want) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, "DELETE FROM registrobr_rdapnum_link WHERE "+col+" = $1::uuid", obj); err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_link: %w", err)
	}
	b := &pgx.Batch{}
	for _, l := range links {
		var via any
		if l.Via != "" {
			via = ids[l.Via]
		}
		b.Queue(`INSERT INTO registrobr_rdapnum_link (entity_uuid, `+col+`, role, via_uuid)
			VALUES ($1::uuid, $2::uuid, $3, $4::uuid) ON CONFLICT DO NOTHING`, ids[l.Handle], obj, l.Role, via)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_link: %w", err)
	}
	return true, nil
}

// reverseKey é a forma canônica de um reverso, para comparar o gravado com o
// novo.
func reverseKey(start, end string, prefix *string, ns []parse.Nameserver, signed *bool, reg, chg *time.Time) string {
	j, _ := json.Marshal(ns)
	f := func(t *time.Time) string {
		if t == nil {
			return "-"
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	p, sg := "-", "-"
	if prefix != nil {
		p = *prefix
	}
	if signed != nil {
		sg = fmt.Sprint(*signed)
	}
	return strings.Join([]string{start, end, p, string(j), sg, f(reg), f(chg)}, "|")
}

// writeReverses regrava as delegações reversas do bloco, se mudaram.
func writeReverses(ctx context.Context, tx pgx.Tx, network string, revs []parse.Reverse) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT host(start_address), host(end_address), prefix::text, nameservers::text, signed, registered_at, changed_at
		  FROM registrobr_rdapnum_reverse WHERE network_uuid = $1::uuid`, network)
	if err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_reverse: %w", err)
	}
	var old []string
	for rows.Next() {
		var start, end, nsText string
		var prefix *string
		var signed *bool
		var reg, chg *time.Time
		if err := rows.Scan(&start, &end, &prefix, &nsText, &signed, &reg, &chg); err != nil {
			rows.Close()
			return false, err
		}
		var ns []parse.Nameserver
		_ = json.Unmarshal([]byte(nsText), &ns)
		if ns == nil {
			ns = []parse.Nameserver{}
		}
		old = append(old, reverseKey(start, end, prefix, ns, signed, reg, chg))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return false, err
	}
	want := make([]string, 0, len(revs))
	for _, r := range revs {
		want = append(want, reverseKey(r.Start.String(), r.End.String(), prefixText(r), r.Nameservers, r.Signed,
			r.RegisteredAt, r.ChangedAt))
	}
	slices.Sort(old)
	slices.Sort(want)
	if slices.Equal(old, want) {
		return false, nil
	}
	if _, err := tx.Exec(ctx, "DELETE FROM registrobr_rdapnum_reverse WHERE network_uuid = $1::uuid", network); err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_reverse: %w", err)
	}
	b := &pgx.Batch{}
	for _, r := range revs {
		ns, err := json.Marshal(r.Nameservers)
		if err != nil {
			return false, err
		}
		b.Queue(`INSERT INTO registrobr_rdapnum_reverse (network_uuid, start_address, end_address, prefix, nameservers,
			       signed, registered_at, changed_at)
			VALUES ($1::uuid, $2::inet, $3::inet, $4::cidr, $5::jsonb, $6, $7, $8)`,
			network, r.Start.String(), r.End.String(), prefixText(r), string(ns), r.Signed, r.RegisteredAt, r.ChangedAt)
	}
	if err := tx.SendBatch(ctx, b).Close(); err != nil {
		return false, fmt.Errorf("registrobr_rdapnum_reverse: %w", err)
	}
	return true, nil
}

func prefixText(r parse.Reverse) *string {
	if r.Prefix == nil {
		return nil
	}
	s := r.Prefix.String()
	return &s
}

// touchSync marca em jobs que as tabelas mudaram (última instrução da
// transação: segura a trava da linha de jobs o menor tempo possível).
func touchSync(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO jobs (app, last_sync_at, consolidated) VALUES ($1, NOW(), 0)
		ON CONFLICT (app) DO UPDATE SET last_sync_at = NOW(), consolidated = 0`, AppName)
	if err != nil {
		return fmt.Errorf("jobs: %w", err)
	}
	return nil
}
