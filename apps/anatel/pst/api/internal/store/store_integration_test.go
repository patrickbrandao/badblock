//go:build integration

package store

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/patrickbrandao/badblock/apps/anatel/pst/api/internal/testdb"
)

// seed é um recorte real do CSV de 2026-09-30, como o collector-anatel-pst
// grava: a Telefônica (um trecho dos serviços, fora de ordem), a Copa Energia
// (nome fantasia, a mesma notificação sob duas outorgas SIR) e a Transat (uma
// notificação dispensada de outorga); mais três prestadoras artificiais com
// %, _ e \ no nome; três execuções (duas aplicadas e uma recusada, a mais
// nova) e a linha do coletor em jobs.
const seed = `
INSERT INTO anatel_pst_provider (document, name, trade_name, street, number, complement, district, postal_code,
                                 city_ibge_code, city, state, phone, email) VALUES
    ('02558157000162', 'TELEFONICA BRASIL S.A.', NULL, 'Avenida Engenheiro Luiz Carlos Berrini', '1376', 'Telefônica Brasil S/A',
     'Cidade Monções', '04571936', 3550308, 'São Paulo', 'SP', '(11) 3430-4532', 'cadastro.fiscal.br@telefonica.com'),
    ('03237583006602', 'Copa Energia Distribuidora de Gas S A', 'Copa Energia', 'Rua Dalton Lahm dos Reis', '260', 'Sala A',
     'Cidade Nova', '95112090', 4305108, 'Caxias do Sul', 'RS', NULL, NULL),
    ('21557625000129', 'TRANSAT TELECOMUNICACOES VIA SATELITE EIRELI', NULL, 'RUA RIO GRANDE DO NORTE', '2668', 'SALA  06',
     'UMUARAMA', '38405321', 3170206, 'Uberlândia', 'MG', NULL, NULL),
    ('00000000000191', 'TESTE 100% Fibra Ltda', 'Under_score', NULL, NULL, NULL, NULL, NULL, NULL, NULL, 'SP', NULL, NULL),
    ('00000000000272', 'Back\Slash Telecom Ltda', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
    ('00000000000353', 'INTERNET RAPIDA LTDA', NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, 'AM', NULL, NULL);

INSERT INTO anatel_pst_service (provider_uuid, entity_type, grant_type, grant_fistel, grant_process, granted_on,
                                service_group, service_code, service_name, notification_fistel, notification_process, notified_on)
SELECT p.uuid, v.entity_type, v.grant_type, v.grant_fistel, v.grant_process, v.granted_on::date,
       v.service_group, v.service_code, v.service_name, v.notification_fistel, v.notification_process, v.notified_on::date
  FROM (VALUES
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Tv por Assinatura', '750', 'Serviço de Acesso Condicionado', '50411491199', '535000240602011', '2014-04-15'),
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Telefonia Fixa', '171', 'SERVICO TELEFONICO FIXO COMUTADO', '50003954846', '535160014082016', '1999-10-01'),
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Telefonia Fixa', '171', 'SERVICO TELEFONICO FIXO COMUTADO', '50001358308', '535000012622003', '1998-03-19'),
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Banda Larga Fixa', '045', 'Serviço de Comunicação Multimídia', '50013053736', '535000020652002', '2003-02-13'),
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Telefonia Móvel', '010', 'SERVIÇO MOVEL PESSOAL', '50409146366', '535000247042011', '2012-04-03'),
    ('02558157000162', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50423150120', '53500036134202046', '2021-01-05',
     'Telefonia Móvel', '010', 'SERVIÇO MOVEL PESSOAL', '50409146285', '535000247042011', '2012-04-03'),
    ('03237583006602', 'Outorgada', 'Serviços de Interesse Restrito - SIR', '50448007169', '53528001299202412', '2024-04-25',
     'Limitado Privado', '019', 'Limitado Privado', '50448153149', '53528001299202412', '2024-05-13'),
    ('03237583006602', 'Outorgada', 'Serviços de Interesse Restrito - SIR', '50455705445', '53500036134202046', '2026-07-15',
     'Limitado Privado', '019', 'Limitado Privado', '50448153149', '53528001299202412', '2024-05-13'),
    ('03237583006602', 'Outorgada', 'Serviços de Interesse Restrito - SIR', '50448007169', '53528001299202412', '2024-04-25',
     'Limitado Privado', '028', 'Limitado Privado Estações Itinerantes', '50455705607', '535280007212004', '2026-07-15'),
    ('21557625000129', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '50426784685', '53500036134202046', '2021-01-05',
     'Banda Larga Fixa', '045', 'Serviço de Comunicação Multimídia', '50416620884', '53500018551201892', '2018-05-29'),
    ('21557625000129', 'Dispensada de Outorga', 'Dispensada de Outorga', NULL, NULL, NULL,
     'Limitado Privado - Dispensa de Outorga', '190', 'Limitado Privado - Dispensa de Autorização', '50418027773', '53500018606201945', '2019-05-10'),
    ('00000000000191', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '00000000001', NULL, '2020-01-01',
     'Banda Larga Fixa', '045', 'Serviço de Comunicação Multimídia', '00000000002', NULL, NULL),
    ('00000000000272', 'Outorgada', 'Serviços de Interesse Coletivo e Restrito - SIC', '00000000003', NULL, '2020-01-01',
     'Banda Larga Fixa', '045', 'Serviço de Comunicação Multimídia', '00000000004', NULL, NULL)
  ) AS v(document, entity_type, grant_type, grant_fistel, grant_process, granted_on, service_group, service_code,
         service_name, notification_fistel, notification_process, notified_on)
  JOIN anatel_pst_provider p ON p.document = v.document;

INSERT INTO anatel_pst_run (status, url, http_status, sha256, bytes, csv_name, csv_sha256, csv_bytes, csv_modified_at,
                            rows, rows_cnpj, rows_cpf, duplicates, skipped, providers, services,
                            provider_inserted, provider_updated, provider_deleted, service_inserted, service_updated, service_deleted,
                            started_at, created_at)
    VALUES (1, 'https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip',
            200, repeat('a', 64), 14700000, 'prestadoras_servicos_telecomunicacoes.csv', repeat('1', 64), 79000000,
            '2026-09-29 09:15:08+00', 276000, 64800, 211200, 10500, 0, 45000, 54200, 45000, 0, 0, 54200, 0, 0,
            NOW() - interval '3 hours', NOW() - interval '3 hours'),
           (1, 'https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip',
            200, repeat('b', 64), 14734094, 'prestadoras_servicos_telecomunicacoes.csv', repeat('2', 64), 79203643,
            '2026-09-30 09:15:08+00', 276261, 64902, 211359, 10588, 0, 45074, 54314, 80, 12, 6, 130, 9, 16,
            NOW() - interval '2 hours', NOW() - interval '2 hours');
INSERT INTO anatel_pst_run (status, url, http_status, sha256, bytes, error, started_at, created_at)
    VALUES (0, 'https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip',
            200, repeat('c', 64), 1000, 'cabeçalho com 23 colunas (esperadas 24)', NOW() - interval '1 hour', NOW() - interval '1 hour');
INSERT INTO jobs (app, last_sync_at, last_check_at) VALUES ('collector-anatel-pst', NOW() - interval '2 hours', NOW());
`

func docs(list []ProviderBrief) []string {
	var out []string
	for _, p := range list {
		out = append(out, p.Document)
	}
	return out
}

func TestQueries(t *testing.T) {
	ctx := context.Background()
	url, admin := testdb.New(t)
	st, err := Open(ctx, url, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Banco vazio: sem versão, sem jobs, sem nada.
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Fatalf("Dataset vazio = %+v, %v", d, err)
	}
	if j, err := st.Job(ctx); j != nil || err != nil {
		t.Fatalf("Job vazio = %+v, %v", j, err)
	}
	if l, err := st.Services(ctx); len(l) != 0 || err != nil {
		t.Fatalf("Services vazio = %+v, %v", l, err)
	}
	if _, err := st.Provider(ctx, "02558157000162"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Provider vazio = %v", err)
	}
	if _, _, err := st.ServiceProviders(ctx, "045", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ServiceProviders vazio = %v", err)
	}
	if l, err := st.Search(ctx, "tele", 10); len(l) != 0 || err != nil {
		t.Fatalf("Search vazio = %+v, %v", l, err)
	}

	if _, err := admin.Exec(ctx, seed); err != nil {
		t.Fatal(err)
	}

	// A versão é a última execução aplicada, não a última linha (recusada).
	d, err := st.Dataset(ctx)
	if err != nil || d == nil {
		t.Fatalf("Dataset = %+v, %v", d, err)
	}
	if d.SHA256 != strings.Repeat("b", 64) || *d.CSVSHA256 != strings.Repeat("2", 64) || !strings.HasSuffix(d.URL, ".zip") ||
		!d.CSVModifiedAt.Equal(time.Date(2026, 9, 30, 9, 15, 8, 0, time.UTC)) || *d.Providers != 45074 || *d.Services != 54314 ||
		d.AppliedAt.IsZero() || len(d.Version) != 36 {
		t.Errorf("Dataset = %+v", d)
	}
	j, err := st.Job(ctx)
	if err != nil || j == nil || j.LastSyncAt == nil || j.LastCheckAt == nil || j.Consolidated != 0 {
		t.Errorf("Job = %+v, %v", j, err)
	}

	// Prestadora com os serviços em ordem de código, data e Fistel.
	p, err := st.Provider(ctx, "02558157000162")
	if err != nil {
		t.Fatal(err)
	}
	if p.Provider.Name != "TELEFONICA BRASIL S.A." || p.Provider.TradeName != nil || *p.Provider.CityIBGECode != 3550308 ||
		*p.Provider.State != "SP" || *p.Provider.Email != "cadastro.fiscal.br@telefonica.com" || p.Provider.CreatedAt.IsZero() {
		t.Errorf("Provider = %+v", p.Provider)
	}
	var order []string
	for _, s := range p.Services {
		order = append(order, s.ServiceCode+" "+s.NotifiedOn.Format(time.DateOnly)+" "+s.NotificationFistel)
	}
	if !slices.Equal(order, []string{
		"010 2012-04-03 50409146285", "010 2012-04-03 50409146366", "045 2003-02-13 50013053736",
		"171 1998-03-19 50001358308", "171 1999-10-01 50003954846", "750 2014-04-15 50411491199",
	}) {
		t.Errorf("ordem dos serviços = %v", order)
	}
	s := p.Services[2]
	if s.ServiceName != "Serviço de Comunicação Multimídia" || s.ServiceGroup != "Banda Larga Fixa" || *s.NotificationProcess != "535000020652002" ||
		s.EntityType != "Outorgada" || *s.GrantFistel != "50423150120" || *s.GrantProcess != "53500036134202046" ||
		s.GrantedOn.Format(time.DateOnly) != "2021-01-05" {
		t.Errorf("045 = %+v", s)
	}
	// Dispensada: outorga nula.
	p, err = st.Provider(ctx, "21557625000129")
	if err != nil || len(p.Services) != 2 || p.Services[1].GrantFistel != nil || p.Services[1].GrantProcess != nil ||
		p.Services[1].GrantedOn != nil || p.Services[1].EntityType != "Dispensada de Outorga" {
		t.Errorf("Transat = %+v, %v", p, err)
	}
	// O store não normaliza o documento.
	for _, doc := range []string{"02.558.157/0001-62", "2558157000162", "", "00000000000000"} {
		if _, err := st.Provider(ctx, doc); !errors.Is(err, ErrNotFound) {
			t.Errorf("Provider(%q) = %v", doc, err)
		}
	}

	// Catálogo: CNPJs distintos e linhas por código.
	cat, err := st.Services(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(cat, []ServiceSummary{
		{"010", "SERVIÇO MOVEL PESSOAL", "Telefonia Móvel", 1, 2},
		{"019", "Limitado Privado", "Limitado Privado", 1, 2},
		{"028", "Limitado Privado Estações Itinerantes", "Limitado Privado", 1, 1},
		{"045", "Serviço de Comunicação Multimídia", "Banda Larga Fixa", 4, 4},
		{"171", "SERVICO TELEFONICO FIXO COMUTADO", "Telefonia Fixa", 1, 2},
		{"190", "Limitado Privado - Dispensa de Autorização", "Limitado Privado - Dispensa de Outorga", 1, 1},
		{"750", "Serviço de Acesso Condicionado", "Tv por Assinatura", 1, 1},
	}) {
		t.Errorf("Services = %+v", cat)
	}

	// Prestadoras de um serviço, em ordem de CNPJ, uma vez cada, com e sem UF.
	info, list, err := st.ServiceProviders(ctx, "045", "")
	if err != nil || info.ServiceName != "Serviço de Comunicação Multimídia" || info.ServiceGroup != "Banda Larga Fixa" ||
		!slices.Equal(docs(list), []string{"00000000000191", "00000000000272", "02558157000162", "21557625000129"}) {
		t.Errorf("045 = %+v %v, %v", info, docs(list), err)
	}
	if list[2].Name != "TELEFONICA BRASIL S.A." || *list[2].City != "São Paulo" || *list[2].State != "SP" || list[1].State != nil {
		t.Errorf("045 itens = %+v", list)
	}
	if _, list, err = st.ServiceProviders(ctx, "045", "SP"); err != nil || !slices.Equal(docs(list), []string{"00000000000191", "02558157000162"}) {
		t.Errorf("045 SP = %v, %v", docs(list), err)
	}
	if _, list, err = st.ServiceProviders(ctx, "045", "AM"); err != nil || list == nil || len(list) != 0 {
		t.Errorf("045 AM = %#v, %v", list, err)
	}
	if _, list, err = st.ServiceProviders(ctx, "019", ""); err != nil || !slices.Equal(docs(list), []string{"03237583006602"}) {
		t.Errorf("019 = %v, %v", docs(list), err)
	}
	for _, code := range []string{"999", "45", "", "045 "} {
		if _, _, err := st.ServiceProviders(ctx, code, ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("ServiceProviders(%q) = %v", code, err)
		}
	}

	// Busca: razão social ou nome fantasia, sem diferenciar maiúsculas, com os
	// curingas literais, em ordem de razão social.
	for term, want := range map[string][]string{
		"telefonica":   {"02558157000162"},
		"copa energia": {"03237583006602"},
		"COPA":         {"03237583006602"},
		"100%":         {"00000000000191"},
		"% fibra":      {"00000000000191"},
		"under_score":  {"00000000000191"}, // pelo nome fantasia
		"t_r":          nil,                // sem o escape casaria INTERNET
		`back\slash`:   {"00000000000272"},
		`\`:            {"00000000000272"},
		"%%%":          nil,
		"___":          nil,
		"zzqqxx":       nil,
		"ltda":         {"00000000000272", "00000000000353", "00000000000191"},
		"uberlândia":   nil, // a cidade não entra na busca
	} {
		got, err := st.Search(ctx, term, 10)
		if err != nil || !slices.Equal(docs(got), want) {
			t.Errorf("Search(%q) = %v, %v; quero %v", term, docs(got), err, want)
		}
	}
	if got, err := st.Search(ctx, "a", 2); err != nil || !slices.Equal(docs(got), []string{"00000000000272", "03237583006602"}) {
		t.Errorf("Search(a, 2) = %v, %v", docs(got), err)
	}

	// Índices: com enable_seqscan = off (tabelas pequenas), cada consulta
	// tem um índice que a atende.
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	for q, idx := range map[string]string{
		`SELECT * FROM anatel_pst_provider WHERE document = '02558157000162'`: "uq_anatel_pst_provider_document",
		`SELECT * FROM anatel_pst_service WHERE provider_uuid = '00000000-0000-0000-0000-000000000000'
		  ORDER BY service_code, notified_on, notification_fistel`: "uq_anatel_pst_service_key",
		`SELECT min(service_name), min(service_group) FROM anatel_pst_service WHERE service_code = '045'`: "ix_anatel_pst_service_service_code",
		`SELECT service_code, min(service_name), min(service_group), count(DISTINCT provider_uuid), count(*)
		  FROM anatel_pst_service GROUP BY service_code ORDER BY service_code`: "ix_anatel_pst_service_service_code",
		`SELECT p.document FROM anatel_pst_provider p WHERE EXISTS (SELECT 1 FROM anatel_pst_service s
		  WHERE s.provider_uuid = p.uuid AND s.service_code = '045') ORDER BY p.document`: "ix_anatel_pst_service_service_code",
		`SELECT document FROM anatel_pst_provider WHERE name ILIKE '%telefonica%' OR trade_name ILIKE '%telefonica%'
		  ORDER BY name, document LIMIT 101`: "ix_anatel_pst_provider_name_trgm",
	} {
		rows, err := tx.Query(ctx, "EXPLAIN "+q)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, line)
		}
		rows.Close()
		if text := strings.Join(plan, "\n"); !strings.Contains(text, idx) {
			t.Errorf("%s: plano sem %s:\n%s", q, idx, text)
		}
	}
	_ = tx.Rollback(ctx)

	// Sem execução aplicada, a versão volta a ser nula.
	if _, err := admin.Exec(ctx, "DELETE FROM anatel_pst_run WHERE status = 1"); err != nil {
		t.Fatal(err)
	}
	if d, err := st.Dataset(ctx); d != nil || err != nil {
		t.Errorf("Dataset sem aplicada = %+v, %v", d, err)
	}
}
