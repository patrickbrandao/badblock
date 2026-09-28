package store

import (
	"context"
	"fmt"
	"time"
)

// buildLockKey serializa as reconstruções dentro do banco (advisory lock de
// transação), além do lock de ciclo.
const buildLockKey int64 = 0x62626275696c64 // "bbbuild"

// BuildStats resume uma reconstrução do central.
type BuildStats struct {
	DatasetID int64
	Baseline  bool
	Changes   int64
	ASNs      int64
	Prefixes  int64
	Holders   int64
	Duration  time.Duration
}

// Build reconstrói as tabelas centrais a partir de ingest numa transação só,
// grava o histórico em registry.change_log, publica uma nova versão do dataset
// e avisa as APIs com NOTIFY badblock_dataset. Enquanto a transação não
// termina, as APIs continuam lendo a versão anterior.
//
// grace é a carência entre um recurso sumir de todas as fontes e ser marcado
// como removido: evita que uma transferência entre RIRs, cujos arquivos saem
// em horários diferentes, vire remoção seguida de recadastro.
func (s *Store) Build(ctx context.Context, grace time.Duration) (BuildStats, error) {
	var st BuildStats
	start := time.Now()

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return st, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, buildLockKey); err != nil {
		return st, err
	}
	if _, err := tx.Exec(ctx, `SET LOCAL work_mem = '256MB'`); err != nil {
		return st, err
	}
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM registry.dataset)`).Scan(&st.Baseline); err != nil {
		return st, err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO registry.dataset (baseline) VALUES ($1) RETURNING id`, st.Baseline).
		Scan(&st.DatasetID); err != nil {
		return st, err
	}

	run := func(name string, stmts []string) error {
		for _, sql := range stmts {
			if _, err := tx.Exec(ctx, sql); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
		return nil
	}
	merge := func(name, sql string) error {
		tag, err := tx.Exec(ctx, sql, st.DatasetID, grace.Seconds(), st.Baseline)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		st.Changes += tag.RowsAffected()
		return nil
	}

	if err := run("preparação", buildPrepare); err != nil {
		return st, err
	}
	if err := merge("holder", mergeHolderSQL); err != nil {
		return st, err
	}
	if err := run("asn", buildASN); err != nil {
		return st, err
	}
	if err := merge("asn", mergeASNSQL); err != nil {
		return st, err
	}
	if err := run("prefix", buildPrefix); err != nil {
		return st, err
	}
	if err := merge("prefix", mergePrefixSQL); err != nil {
		return st, err
	}
	if err := run("finalização", buildFinish); err != nil {
		return st, err
	}

	st.Duration = time.Since(start)
	if err := tx.QueryRow(ctx, `
		UPDATE registry.dataset
		   SET asn_count    = (SELECT count(*) FROM registry.asn    WHERE removed_at IS NULL),
		       prefix_count = (SELECT count(*) FROM registry.prefix WHERE removed_at IS NULL),
		       holder_count = (SELECT count(*) FROM registry.holder WHERE removed_at IS NULL),
		       changes      = $2,
		       duration_ms  = $3
		 WHERE id = $1
		RETURNING asn_count, prefix_count, holder_count`,
		st.DatasetID, st.Changes, st.Duration.Milliseconds()).Scan(&st.ASNs, &st.Prefixes, &st.Holders); err != nil {
		return st, err
	}
	// Estatísticas frescas depois de mudanças grandes, para os planos da API.
	if st.Baseline || st.Changes > 50000 {
		if _, err := tx.Exec(ctx, `ANALYZE registry.holder, registry.asn, registry.prefix, registry.asn_block`); err != nil {
			return st, err
		}
	}
	// O NOTIFY só é entregue no commit, junto com a nova versão.
	if _, err := tx.Exec(ctx, `SELECT pg_notify('badblock_dataset', ($1::bigint)::text)`, st.DatasetID); err != nil {
		return st, err
	}
	return st, tx.Commit(ctx)
}

// buildPrepare monta as tabelas temporárias que alimentam os três MERGE.
var buildPrepare = []string{
	// Data do arquivo de cada RIR: quando um recurso aparece em dois RIRs
	// durante uma transferência, vence o arquivo mais novo.
	`CREATE TEMP TABLE b_rir_date ON COMMIT DROP AS
	 SELECT substr(source_id, 5) AS rir, file_date
	   FROM ingest.source_state
	  WHERE source_id LIKE 'rir-%'`,

	// URL base do RDAP de cada RIR, identificada pelo host no bootstrap. O RDAP
	// de um recurso é o do RIR que o delegou: o bootstrap IPv4 é por /8, e em
	// blocos legados (45/8 é da ARIN) a LACNIC delega partes.
	`CREATE TEMP TABLE b_rir_rdap ON COMMIT DROP AS
	 SELECT DISTINCT ON (rir) rir, base_url
	   FROM (SELECT CASE
	                  WHEN base_url LIKE '%afrinic.net/%' THEN 'afrinic'
	                  WHEN base_url LIKE '%apnic.net/%'   THEN 'apnic'
	                  WHEN base_url LIKE '%arin.net/%'    THEN 'arin'
	                  WHEN base_url LIKE '%lacnic.net/%'  THEN 'lacnic'
	                  WHEN base_url LIKE '%ripe.net/%'    THEN 'ripencc'
	                END AS rir, base_url
	           FROM ingest.rdap_service) x
	  WHERE rir IS NOT NULL
	  ORDER BY rir, base_url`,

	// Um ASN por linha (as faixas são expandidas), já resolvido entre RIRs.
	`CREATE TEMP TABLE b_asn_reg ON COMMIT DROP AS
	 SELECT DISTINCT ON (g.asn) g.asn, d.rir, d.cc, d.status, d.reg_date, d.opaque_id
	   FROM ingest.delegation d
	  CROSS JOIN LATERAL generate_series(d.asn_first, d.asn_last) AS g(asn)
	   LEFT JOIN b_rir_date r ON r.rir = d.rir
	  WHERE d.rtype = 'asn'
	  ORDER BY g.asn, r.file_date DESC NULLS LAST, (d.status IN ('allocated', 'assigned')) DESC, d.rir`,
	`CREATE UNIQUE INDEX ON b_asn_reg (asn)`,
	`ANALYZE b_asn_reg`,

	// Um prefixo por CIDR das delegações, também resolvido entre RIRs.
	`CREATE TEMP TABLE b_pfx_reg ON COMMIT DROP AS
	 SELECT DISTINCT ON (c.cidr) c.cidr AS prefix, d.rir, d.rtype, d.cc, d.status, d.reg_date,
	        d.opaque_id, d.start, d.value, cardinality(d.cidrs) AS parts
	   FROM ingest.delegation d
	  CROSS JOIN LATERAL unnest(d.cidrs) AS c(cidr)
	   LEFT JOIN b_rir_date r ON r.rir = d.rir
	  WHERE d.rtype IN ('ipv4', 'ipv6')
	  ORDER BY c.cidr, r.file_date DESC NULLS LAST, (d.status IN ('allocated', 'assigned')) DESC, d.rir`,
	`CREATE UNIQUE INDEX ON b_pfx_reg (prefix)`,
	`ANALYZE b_pfx_reg`,

	// Vínculos explícitos ASN ↔ prefixo do NIC.br.
	`CREATE TEMP TABLE b_nicbr_link ON COMMIT DROP AS
	 SELECT prefix, array_agg(DISTINCT asn ORDER BY asn) AS asns
	   FROM ingest.nicbr_prefix
	  GROUP BY prefix`,
	`CREATE UNIQUE INDEX ON b_nicbr_link (prefix)`,

	// Titulares: país mais frequente e contagens.
	`CREATE TEMP TABLE b_holder_base ON COMMIT DROP AS
	 SELECT rir, opaque_id,
	        mode() WITHIN GROUP (ORDER BY cc) FILTER (WHERE cc NOT IN ('', 'ZZ')) AS country,
	        coalesce(sum(asn_last - asn_first + 1) FILTER (WHERE rtype = 'asn'), 0)::int AS asn_count,
	        coalesce(sum(cardinality(cidrs)) FILTER (WHERE rtype = 'ipv4'), 0)::int  AS prefix4_count,
	        (count(*) FILTER (WHERE rtype = 'ipv6'))::int                            AS prefix6_count,
	        coalesce(sum(value) FILTER (WHERE rtype = 'ipv4'), 0)::bigint            AS ipv4_addresses
	   FROM ingest.delegation
	  WHERE opaque_id <> ''
	  GROUP BY rir, opaque_id`,

	// Nome e documento do NIC.br para titulares da LACNIC. O voto pelo ASN
	// só vale para ASN da LACNIC; ASNs estrangeiros listados pelo NIC.br
	// (AS8075, AS174...) dão nome ao titular dos blocos, nunca ao ASN.
	`CREATE TEMP TABLE b_holder_nicbr ON COMMIT DROP AS
	 WITH votes AS (
	     SELECT a.opaque_id, n.asn, 0 AS prio, 1::bigint AS weight
	       FROM ingest.nicbr_asn n
	       JOIN b_asn_reg a ON a.asn = n.asn
	      WHERE a.rir = 'lacnic' AND a.opaque_id <> ''
	     UNION ALL
	     SELECT p.opaque_id, l.asn, 1 AS prio, count(*) AS weight
	       FROM ingest.nicbr_prefix l
	       JOIN b_pfx_reg p ON p.prefix = l.prefix
	      WHERE p.rir = 'lacnic' AND p.opaque_id <> ''
	      GROUP BY p.opaque_id, l.asn
	 )
	 SELECT DISTINCT ON (v.opaque_id) v.opaque_id, n.name, nullif(n.document, '') AS document
	   FROM votes v
	   JOIN ingest.nicbr_asn n ON n.asn = v.asn
	  ORDER BY v.opaque_id, v.prio, v.weight DESC, v.asn`,

	// Nome inferido: o nome mais frequente entre os ASNs do titular no asn.txt.
	`CREATE TEMP TABLE b_holder_inferred ON COMMIT DROP AS
	 SELECT DISTINCT ON (rir, opaque_id) rir, opaque_id, name
	   FROM (SELECT a.rir, a.opaque_id, n.name, count(*) AS votes, min(a.asn) AS first_asn
	           FROM b_asn_reg a
	           JOIN ingest.asname n ON n.asn = a.asn
	          WHERE a.opaque_id <> '' AND n.name <> ''
	          GROUP BY a.rir, a.opaque_id, n.name) x
	  ORDER BY rir, opaque_id, votes DESC, first_asn`,

	`CREATE TEMP TABLE b_holder ON COMMIT DROP AS
	 SELECT h.rir, h.opaque_id,
	        coalesce(nb.name, hi.name) AS name,
	        CASE WHEN nb.name IS NOT NULL THEN 'nicbr'
	             WHEN hi.name IS NOT NULL THEN 'inferred' END AS name_source,
	        nb.document,
	        h.country, h.asn_count, h.prefix4_count, h.prefix6_count, h.ipv4_addresses
	   FROM b_holder_base h
	   LEFT JOIN b_holder_nicbr nb ON h.rir = 'lacnic' AND nb.opaque_id = h.opaque_id
	   LEFT JOIN b_holder_inferred hi ON hi.rir = h.rir AND hi.opaque_id = h.opaque_id`,
}

// Parâmetros comuns dos três MERGE: $1 = id do dataset, $2 = carência em
// segundos, $3 = baseline (primeira carga: sem eventos 'insert' por linha).
//
// Em cada MERGE: linhas novas são inseridas; linhas que mudaram, ou que
// estavam sumidas e voltaram, são atualizadas; linhas ausentes ganham
// missing_since e, vencida a carência, removed_at. O RETURNING com old/new
// (PG 18) alimenta o change_log só com mudanças visíveis: marcar missing_since
// não gera evento.

const mergeHolderSQL = `
WITH m AS (
    MERGE INTO registry.holder AS t
    USING b_holder AS n
       ON t.rir = n.rir AND t.opaque_id = n.opaque_id
    WHEN MATCHED AND (t.removed_at IS NOT NULL OR t.missing_since IS NOT NULL
                      OR ROW(t.name, t.name_source, t.document, t.country,
                             t.asn_count, t.prefix4_count, t.prefix6_count, t.ipv4_addresses)
                         IS DISTINCT FROM
                         ROW(n.name, n.name_source, n.document, n.country,
                             n.asn_count, n.prefix4_count, n.prefix6_count, n.ipv4_addresses)) THEN
        UPDATE SET name = n.name, name_source = n.name_source, document = n.document, country = n.country,
                   asn_count = n.asn_count, prefix4_count = n.prefix4_count,
                   prefix6_count = n.prefix6_count, ipv4_addresses = n.ipv4_addresses,
                   updated_at = CASE WHEN ROW(t.name, t.name_source, t.document, t.country)
                                          IS DISTINCT FROM ROW(n.name, n.name_source, n.document, n.country)
                                     THEN now() ELSE t.updated_at END,
                   missing_since = NULL, removed_at = NULL
    WHEN NOT MATCHED THEN
        INSERT (rir, opaque_id, name, name_source, document, country,
                asn_count, prefix4_count, prefix6_count, ipv4_addresses)
        VALUES (n.rir, n.opaque_id, n.name, n.name_source, n.document, n.country,
                n.asn_count, n.prefix4_count, n.prefix6_count, n.ipv4_addresses)
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL
         AND t.missing_since <= now() - make_interval(secs => $2) THEN
        UPDATE SET removed_at = now()
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL AND t.missing_since IS NULL THEN
        UPDATE SET missing_since = now()
    RETURNING merge_action() AS action,
              coalesce(new.rir, old.rir) || ':' || coalesce(new.opaque_id, old.opaque_id) AS key,
              old.removed_at AS old_removed, new.removed_at AS new_removed,
              jsonb_build_object('name', old.name, 'name_source', old.name_source,
                                 'document', old.document, 'country', old.country) AS before,
              jsonb_build_object('name', new.name, 'name_source', new.name_source,
                                 'document', new.document, 'country', new.country) AS after
)
INSERT INTO registry.change_log (dataset_id, entity, key, action, before, after)
SELECT $1::bigint, 'holder', x.key, x.act,
       CASE WHEN x.act <> 'insert' THEN x.before END,
       CASE WHEN x.act <> 'remove' THEN x.after END
  FROM (SELECT m.*,
               CASE WHEN action = 'INSERT' THEN 'insert'
                    WHEN old_removed IS NOT NULL AND new_removed IS NULL THEN 'restore'
                    WHEN old_removed IS NULL AND new_removed IS NOT NULL THEN 'remove'
                    WHEN before IS DISTINCT FROM after THEN 'update'
               END AS act
          FROM m) x
 WHERE x.act IS NOT NULL AND NOT (x.act = 'insert' AND $3::boolean)`

// buildASN monta os ASNs finais: titular, nome (NIC.br para ASNs LACNIC/BR,
// asn.txt para os demais) e URL RDAP do RIR que delegou.
var buildASN = []string{
	`CREATE TEMP TABLE b_holder_id ON COMMIT DROP AS
	 SELECT id, rir, opaque_id FROM registry.holder`,
	`CREATE UNIQUE INDEX ON b_holder_id (rir, opaque_id)`,
	`CREATE UNIQUE INDEX ON b_holder_id (id)`,
	`ANALYZE b_holder_id`,
	`CREATE TEMP TABLE b_asn ON COMMIT DROP AS
	 SELECT a.asn, a.rir,
	        nullif(nullif(a.cc, ''), 'ZZ') AS country,
	        a.status,
	        a.reg_date AS registered,
	        h.id AS holder_id,
	        CASE WHEN a.rir = 'lacnic' AND a.cc = 'BR' AND nb.name IS NOT NULL THEN nb.name
	             ELSE coalesce(nullif(an.name, ''), nullif(an.handle, '')) END AS name,
	        CASE WHEN a.rir = 'lacnic' AND a.cc = 'BR' AND nb.name IS NOT NULL THEN 'nicbr'
	             WHEN coalesce(nullif(an.name, ''), nullif(an.handle, '')) IS NOT NULL THEN 'asnames'
	        END AS name_source,
	        nullif(an.handle, '') AS handle,
	        rr.base_url || 'autnum/' || a.asn AS rdap_url
	   FROM b_asn_reg a
	   LEFT JOIN b_holder_id h ON h.rir = a.rir AND h.opaque_id = a.opaque_id
	   LEFT JOIN ingest.nicbr_asn nb ON nb.asn = a.asn
	   LEFT JOIN ingest.asname an ON an.asn = a.asn
	   LEFT JOIN b_rir_rdap rr ON rr.rir = a.rir`,
}

const mergeASNSQL = `
WITH m AS (
    MERGE INTO registry.asn AS t
    USING b_asn AS n
       ON t.asn = n.asn
    WHEN MATCHED AND (t.removed_at IS NOT NULL OR t.missing_since IS NOT NULL
                      OR ROW(t.rir, t.country, t.status, t.registered, t.holder_id,
                             t.name, t.name_source, t.handle, t.rdap_url)
                         IS DISTINCT FROM
                         ROW(n.rir, n.country, n.status, n.registered, n.holder_id,
                             n.name, n.name_source, n.handle, n.rdap_url)) THEN
        UPDATE SET rir = n.rir, country = n.country, status = n.status, registered = n.registered,
                   holder_id = n.holder_id, name = n.name, name_source = n.name_source,
                   handle = n.handle, rdap_url = n.rdap_url,
                   updated_at = CASE WHEN ROW(t.rir, t.country, t.status, t.registered, t.holder_id,
                                              t.name, t.name_source, t.handle)
                                          IS DISTINCT FROM
                                          ROW(n.rir, n.country, n.status, n.registered, n.holder_id,
                                              n.name, n.name_source, n.handle)
                                     THEN now() ELSE t.updated_at END,
                   missing_since = NULL, removed_at = NULL
    WHEN NOT MATCHED THEN
        INSERT (asn, rir, country, status, registered, holder_id, name, name_source, handle, rdap_url)
        VALUES (n.asn, n.rir, n.country, n.status, n.registered, n.holder_id,
                n.name, n.name_source, n.handle, n.rdap_url)
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL
         AND t.missing_since <= now() - make_interval(secs => $2) THEN
        UPDATE SET removed_at = now()
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL AND t.missing_since IS NULL THEN
        UPDATE SET missing_since = now()
    RETURNING merge_action() AS action, coalesce(new.asn, old.asn) AS asn,
              old.removed_at AS old_removed, new.removed_at AS new_removed,
              old.rir AS o_rir, new.rir AS n_rir,
              old.country AS o_country, new.country AS n_country,
              old.status AS o_status, new.status AS n_status,
              old.registered AS o_registered, new.registered AS n_registered,
              old.holder_id AS o_holder, new.holder_id AS n_holder,
              old.name AS o_name, new.name AS n_name,
              old.name_source AS o_name_source, new.name_source AS n_name_source,
              old.handle AS o_handle, new.handle AS n_handle
)
INSERT INTO registry.change_log (dataset_id, entity, key, action, before, after)
SELECT $1::bigint, 'asn', x.asn::text, x.act,
       CASE WHEN x.act <> 'insert' THEN x.before END,
       CASE WHEN x.act <> 'remove' THEN x.after END
  FROM (SELECT m.asn,
               jsonb_build_object('rir', m.o_rir, 'country', m.o_country, 'status', m.o_status,
                                  'registered', m.o_registered, 'holder', ho.rir || ':' || ho.opaque_id,
                                  'name', m.o_name, 'name_source', m.o_name_source, 'handle', m.o_handle) AS before,
               jsonb_build_object('rir', m.n_rir, 'country', m.n_country, 'status', m.n_status,
                                  'registered', m.n_registered, 'holder', hn.rir || ':' || hn.opaque_id,
                                  'name', m.n_name, 'name_source', m.n_name_source, 'handle', m.n_handle) AS after,
               CASE WHEN m.action = 'INSERT' THEN 'insert'
                    WHEN m.old_removed IS NOT NULL AND m.new_removed IS NULL THEN 'restore'
                    WHEN m.old_removed IS NULL AND m.new_removed IS NOT NULL THEN 'remove'
                    WHEN ROW(m.o_rir, m.o_country, m.o_status, m.o_registered, m.o_holder,
                             m.o_name, m.o_name_source, m.o_handle)
                         IS DISTINCT FROM
                         ROW(m.n_rir, m.n_country, m.n_status, m.n_registered, m.n_holder,
                             m.n_name, m.n_name_source, m.n_handle) THEN 'update'
               END AS act
          FROM m
          LEFT JOIN b_holder_id ho ON ho.id = m.o_holder
          LEFT JOIN b_holder_id hn ON hn.id = m.n_holder) x
 WHERE x.act IS NOT NULL AND NOT (x.act = 'insert' AND $3::boolean)`

// buildPrefix monta os prefixos de todos os níveis.
var buildPrefix = []string{
	`CREATE TEMP TABLE b_prefix (
	     level              text NOT NULL,
	     prefix             cidr NOT NULL,
	     rir                text,
	     country            text,
	     status             text,
	     registered         date,
	     holder_id          bigint,
	     nicbr_asns         bigint[],
	     designation        text,
	     special_rfc        text,
	     globally_reachable boolean,
	     source_start       text,
	     source_value       bigint,
	     whois              text,
	     rdap_url           text
	 ) ON COMMIT DROP`,

	// Delegações dos RIRs. source_start/source_value só quando o CIDR veio da
	// divisão de um registro IPv4 fora de CIDR.
	`INSERT INTO b_prefix (level, prefix, rir, country, status, registered, holder_id, nicbr_asns,
	                       source_start, source_value, rdap_url)
	 SELECT 'rir', p.prefix, p.rir, nullif(nullif(p.cc, ''), 'ZZ'), p.status, p.reg_date, h.id, nl.asns,
	        CASE WHEN p.parts > 1 THEN p.start END,
	        CASE WHEN p.parts > 1 THEN p.value END,
	        rr.base_url || 'ip/' || host(p.prefix) || '/' || masklen(p.prefix)
	   FROM b_pfx_reg p
	   LEFT JOIN b_holder_id h ON h.rir = p.rir AND h.opaque_id = p.opaque_id
	   LEFT JOIN b_nicbr_link nl ON nl.prefix = p.prefix
	   LEFT JOIN b_rir_rdap rr ON rr.rir = p.rir`,

	// Blocos do NIC.br sem delegação idêntica da LACNIC. Em 2026-09 todos os
	// 21.991 blocos coincidem; este nível existe para não perder o vínculo se
	// o NIC.br passar a listar sub-blocos.
	`INSERT INTO b_prefix (level, prefix, rir, country, holder_id, nicbr_asns, rdap_url)
	 SELECT 'nicbr', nl.prefix, 'lacnic', 'BR',
	        (SELECT h.id
	           FROM b_asn_reg a
	           JOIN b_holder_id h ON h.rir = a.rir AND h.opaque_id = a.opaque_id
	          WHERE a.asn = ANY (nl.asns) AND a.rir = 'lacnic'
	          ORDER BY a.asn LIMIT 1),
	        nl.asns,
	        (SELECT base_url FROM b_rir_rdap WHERE rir = 'lacnic') || 'ip/' || host(nl.prefix) || '/' || masklen(nl.prefix)
	   FROM b_nicbr_link nl
	  WHERE NOT EXISTS (SELECT 1 FROM b_pfx_reg p WHERE p.prefix = nl.prefix)`,

	// Blocos de topo da IANA. O RIR sai do WHOIS: nos blocos legados a
	// designação é o nome da organização ("Apple Computer Inc.").
	`INSERT INTO b_prefix (level, prefix, rir, status, registered, designation, whois)
	 SELECT 'iana', b.prefix,
	        CASE b.whois WHEN 'whois.afrinic.net' THEN 'afrinic' WHEN 'whois.apnic.net' THEN 'apnic'
	                     WHEN 'whois.arin.net' THEN 'arin' WHEN 'whois.lacnic.net' THEN 'lacnic'
	                     WHEN 'whois.ripe.net' THEN 'ripencc' END,
	        b.status, b.reg_date, b.designation, nullif(b.whois, '')
	   FROM ingest.iana_ip_block b`,

	`INSERT INTO b_prefix (level, prefix, registered, designation, special_rfc, globally_reachable)
	 SELECT DISTINCT ON (s.prefix) 'special', s.prefix, s.alloc_date, s.name, nullif(s.rfc, ''), s.globally_reachable
	   FROM ingest.iana_special_ip s
	  ORDER BY s.prefix, s.source_id`,

	`CREATE UNIQUE INDEX ON b_prefix (level, prefix)`,
	`ANALYZE b_prefix`,
}

const mergePrefixSQL = `
WITH m AS (
    MERGE INTO registry.prefix AS t
    USING b_prefix AS n
       ON t.level = n.level AND t.prefix = n.prefix
    WHEN MATCHED AND (t.removed_at IS NOT NULL OR t.missing_since IS NOT NULL
                      OR ROW(t.rir, t.country, t.status, t.registered, t.holder_id, t.nicbr_asns,
                             t.designation, t.special_rfc, t.globally_reachable, t.source_start,
                             t.source_value, t.whois, t.rdap_url)
                         IS DISTINCT FROM
                         ROW(n.rir, n.country, n.status, n.registered, n.holder_id, n.nicbr_asns,
                             n.designation, n.special_rfc, n.globally_reachable, n.source_start,
                             n.source_value, n.whois, n.rdap_url)) THEN
        UPDATE SET rir = n.rir, country = n.country, status = n.status, registered = n.registered,
                   holder_id = n.holder_id, nicbr_asns = n.nicbr_asns, designation = n.designation,
                   special_rfc = n.special_rfc, globally_reachable = n.globally_reachable,
                   source_start = n.source_start, source_value = n.source_value,
                   whois = n.whois, rdap_url = n.rdap_url,
                   updated_at = CASE WHEN ROW(t.rir, t.country, t.status, t.registered, t.holder_id,
                                              t.nicbr_asns, t.designation, t.globally_reachable)
                                          IS DISTINCT FROM
                                          ROW(n.rir, n.country, n.status, n.registered, n.holder_id,
                                              n.nicbr_asns, n.designation, n.globally_reachable)
                                     THEN now() ELSE t.updated_at END,
                   missing_since = NULL, removed_at = NULL
    WHEN NOT MATCHED THEN
        INSERT (level, prefix, rir, country, status, registered, holder_id, nicbr_asns, designation,
                special_rfc, globally_reachable, source_start, source_value, whois, rdap_url)
        VALUES (n.level, n.prefix, n.rir, n.country, n.status, n.registered, n.holder_id, n.nicbr_asns,
                n.designation, n.special_rfc, n.globally_reachable, n.source_start, n.source_value,
                n.whois, n.rdap_url)
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL
         AND t.missing_since <= now() - make_interval(secs => $2) THEN
        UPDATE SET removed_at = now()
    WHEN NOT MATCHED BY SOURCE AND t.removed_at IS NULL AND t.missing_since IS NULL THEN
        UPDATE SET missing_since = now()
    RETURNING merge_action() AS action,
              coalesce(new.level, old.level) AS level, coalesce(new.prefix, old.prefix) AS prefix,
              old.removed_at AS old_removed, new.removed_at AS new_removed,
              old.rir AS o_rir, new.rir AS n_rir,
              old.country AS o_country, new.country AS n_country,
              old.status AS o_status, new.status AS n_status,
              old.registered AS o_registered, new.registered AS n_registered,
              old.holder_id AS o_holder, new.holder_id AS n_holder,
              old.nicbr_asns AS o_asns, new.nicbr_asns AS n_asns,
              old.designation AS o_designation, new.designation AS n_designation,
              old.globally_reachable AS o_global, new.globally_reachable AS n_global
)
INSERT INTO registry.change_log (dataset_id, entity, key, level, action, before, after)
SELECT $1::bigint, 'prefix', x.prefix::text, x.level, x.act,
       CASE WHEN x.act <> 'insert' THEN x.before END,
       CASE WHEN x.act <> 'remove' THEN x.after END
  FROM (SELECT m.level, m.prefix,
               jsonb_build_object('rir', m.o_rir, 'country', m.o_country, 'status', m.o_status,
                                  'registered', m.o_registered, 'holder', ho.rir || ':' || ho.opaque_id,
                                  'nicbr_asns', to_jsonb(m.o_asns), 'designation', m.o_designation,
                                  'globally_reachable', m.o_global) AS before,
               jsonb_build_object('rir', m.n_rir, 'country', m.n_country, 'status', m.n_status,
                                  'registered', m.n_registered, 'holder', hn.rir || ':' || hn.opaque_id,
                                  'nicbr_asns', to_jsonb(m.n_asns), 'designation', m.n_designation,
                                  'globally_reachable', m.n_global) AS after,
               CASE WHEN m.action = 'INSERT' THEN 'insert'
                    WHEN m.old_removed IS NOT NULL AND m.new_removed IS NULL THEN 'restore'
                    WHEN m.old_removed IS NULL AND m.new_removed IS NOT NULL THEN 'remove'
                    WHEN ROW(m.o_rir, m.o_country, m.o_status, m.o_registered, m.o_holder,
                             m.o_asns, m.o_designation, m.o_global)
                         IS DISTINCT FROM
                         ROW(m.n_rir, m.n_country, m.n_status, m.n_registered, m.n_holder,
                             m.n_asns, m.n_designation, m.n_global) THEN 'update'
               END AS act
          FROM m
          LEFT JOIN b_holder_id ho ON ho.id = m.o_holder
          LEFT JOIN b_holder_id hn ON hn.id = m.n_holder) x
 WHERE x.act IS NOT NULL AND NOT (x.act = 'insert' AND $3::boolean)`

// buildFinish reconstrói as tabelas sem histórico: blocos de ASN da IANA e a
// lista de blocos de cache com prefixos mais específicos que /24 e /48.
var buildFinish = []string{
	`DELETE FROM registry.asn_block`,
	`INSERT INTO registry.asn_block (level, asn_first, asn_last, rir, designation, reference, registered, whois, rdap_base)
	 SELECT 'iana', asn_first, asn_last,
	        CASE whois WHEN 'whois.afrinic.net' THEN 'afrinic' WHEN 'whois.apnic.net' THEN 'apnic'
	                   WHEN 'whois.arin.net' THEN 'arin' WHEN 'whois.lacnic.net' THEN 'lacnic'
	                   WHEN 'whois.ripe.net' THEN 'ripencc' END,
	        description, nullif(reference, ''), reg_date, nullif(whois, ''), nullif(rdap, '')
	   FROM ingest.iana_asn_block
	 UNION ALL
	 SELECT 'special', asn_first, asn_last, NULL, reason, nullif(reference, ''), NULL, NULL, NULL
	   FROM ingest.iana_special_asn`,
	`DELETE FROM registry.cache_bucket_exception`,
	`INSERT INTO registry.cache_bucket_exception (bucket)
	 SELECT DISTINCT set_masklen(prefix, CASE WHEN family = 4 THEN 24 ELSE 48 END)
	   FROM registry.prefix
	  WHERE removed_at IS NULL
	    AND masklen(prefix) > CASE WHEN family = 4 THEN 24 ELSE 48 END`,
}
