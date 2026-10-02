// APIs do BadBlock: o menu "APIs", o rodapé, a página /apis e uma página por
// API em /apis/<slug>. Fonte das informações: specs/fontes/<fonte>/README.md e
// specs/fontes/<fonte>/api.md. Ao incluir uma API, inclua o slug também nos
// lighttpd*.conf (regra de url.rewrite-once); o tests/run.mjs confere.

export const API_HOST = 'https://api.badblock.net.br';

// Rotas que toda API tem (specs/padroes/api.md), abaixo do caminho base.
export const COMMON_ROUTES = [
  { path: '/', desc: 'Índice: versão da API, caminho base e a lista de endpoints.' },
  { path: '/meta', desc: 'Versão do dataset carregado, origem dos dados e o estado do coletor.' },
  { path: '/status', desc: 'Saúde da API (também em /health); 503 quando o banco está fora.' },
  { path: '/ping', desc: 'Responde pong, em texto puro.' },
  { path: '/openapi.yaml', desc: 'Manifesto OpenAPI 3.1 da API.' },
];

const rirRoutes = (p, ex) => [
  { path: `${p}/asn/{asn}`, desc: 'A delegação que contém o ASN: faixa, país, data, status e titular (opaque-id).', example: `${p}/asn/${ex.asn}` },
  { path: `${p}/ip/{ip}`, desc: 'O bloco IPv4 ou IPv6 delegado que contém o endereço.', example: `${p}/ip/${ex.ip}` },
  { path: `${p}/prefix/{ip}/{len}`, desc: 'O bloco delegado que contém o prefixo, dizendo se a correspondência é exata.', example: `${p}/prefix/${ex.prefix}` },
  { path: `${p}/holder/{opaque_id}`, desc: 'Tudo o que um titular recebeu: ASNs e blocos IPv4 e IPv6.', example: ex.holder ? `${p}/holder/${ex.holder}` : null },
];

const rir = ({ slug, name, region, file, ex, extra }) => ({
  slug,
  path: `/${slug}`,
  name,
  group: 'rir',
  menuDesc: `Delegações de ASNs e blocos IP — ${region}`,
  summary: `Delegações de ASNs e blocos IPv4/IPv6 do ${name}, o registro regional da internet (RIR) de ${region}.`,
  about: [
    `O ${name} publica todo dia o arquivo delegated-extended: uma linha por faixa de ASNs ou bloco de endereços IP que ele administra, com o país, a data da delegação, o status (allocated, assigned, available, reserved) e o titular, identificado por um opaque-id.`,
    `O BadBlock baixa o arquivo, confere se mudou e carrega cada versão no banco. A API responde a que delegação pertence um ASN, um IP ou um prefixo, e o que cada titular recebeu.`,
    ...(extra ? [extra] : []),
  ],
  data: ['Faixas de ASNs', 'Blocos IPv4 e IPv6', 'País, data e status de cada delegação', 'Titular (opaque-id) e tudo o que ele recebeu'],
  source: file,
  routes: rirRoutes(`/${slug}`, ex),
});

export const APIS = [
  rir({
    slug: 'afrinic', name: 'AFRINIC', region: 'África',
    file: 'https://ftp.afrinic.net/pub/stats/afrinic/delegated-afrinic-extended-latest',
    ex: { asn: '37100', ip: '196.216.2.1', prefix: '196.216.2.0/24', holder: 'F365C741' },
  }),
  rir({
    slug: 'apnic', name: 'APNIC', region: 'Ásia-Pacífico',
    file: 'https://ftp.apnic.net/stats/apnic/delegated-apnic-extended-latest',
    ex: { asn: '4608', ip: '1.1.1.1', prefix: '1.1.1.0/24', holder: 'A91DC5BE' },
  }),
  rir({
    slug: 'arin', name: 'ARIN', region: 'América do Norte',
    file: 'https://ftp.arin.net/pub/stats/arin/delegated-arin-extended-latest',
    ex: { asn: '7018', ip: '8.8.8.8', prefix: '8.8.8.0/24', holder: '81e05477cc28a48ed3088e3408139c2a' },
  }),
  rir({
    slug: 'lacnic', name: 'LACNIC', region: 'América Latina e Caribe',
    file: 'https://ftp.lacnic.net/pub/stats/lacnic/delegated-lacnic-extended-latest',
    ex: { asn: '61605', ip: '179.63.179.10', prefix: '179.63.179.0/24', holder: '258500' },
  }),
  rir({
    slug: 'ripencc', name: 'RIPE NCC', region: 'Europa, Oriente Médio e Ásia Central',
    file: 'https://ftp.ripe.net/pub/stats/ripencc/delegated-ripencc-extended-latest',
    ex: { asn: '3333', ip: '193.0.6.139', prefix: '193.0.0.0/21', holder: null },
    extra: 'No RIPE NCC o opaque-id é um UUID novo a cada arquivo diário: consulte o titular de hoje pela resposta de /asn ou /ip antes de usar /holder.',
  }),
  {
    slug: 'iana',
    path: '/iana',
    name: 'IANA',
    group: 'rir',
    menuDesc: 'Registros de numeração, bogons e bootstrap RDAP',
    summary: 'Os registros de numeração da IANA, acima dos cinco RIRs: a quem foi entregue cada faixa de ASN e cada bloco IP, o que é de uso especial (bogon) e qual servidor RDAP responde por cada faixa.',
    about: [
      'A IANA publica 10 arquivos pequenos: as faixas de ASN e os blocos IPv4 (/8) e IPv6 unicast entregues a cada RIR, os registros de uso especial de IPv4, IPv6 e ASN, e o bootstrap RDAP (RFC 9224). O BadBlock trata os 10 como um dataset só: aplica todos juntos, ou nenhum.',
      'Para um ASN, um IP ou um prefixo, a API diz o que os registros da IANA dizem — a faixa ou o bloco (RIR, status, WHOIS, RDAP), os registros de uso especial que o contêm e o servidor RDAP — e, para IP e prefixo, se é bogon. Uma entrada válida nunca dá 404: o que a IANA não diz sai nulo.',
    ],
    data: ['Faixas de ASN por RIR', 'Blocos IPv4 /8 e IPv6 unicast', 'Endereços e ASNs de uso especial (bogons)', 'Bootstrap RDAP'],
    source: 'https://www.iana.org/numbers',
    routes: [
      { path: '/iana/asn/{asn}', desc: 'A faixa da IANA que contém o ASN, se é de uso especial e o servidor RDAP.', example: '/iana/asn/61605' },
      { path: '/iana/ip/{ip}', desc: 'O bloco que contém o endereço, os registros de uso especial, o RDAP e se é bogon.', example: '/iana/ip/10.0.0.1' },
      { path: '/iana/prefix/{ip}/{len}', desc: 'O mesmo para um prefixo.', example: '/iana/prefix/2001:db8::/48' },
      { path: '/iana/asns', desc: 'Todas as faixas de ASN.', example: '/iana/asns' },
      { path: '/iana/ipv4', desc: 'Os 256 blocos /8 do IPv4.', example: '/iana/ipv4' },
      { path: '/iana/ipv6', desc: 'Os blocos IPv6 unicast.', example: '/iana/ipv6' },
      { path: '/iana/special', desc: 'Os registros de uso especial de IPv4, IPv6 e ASN.', example: '/iana/special' },
      { path: '/iana/rdap', desc: 'O bootstrap RDAP inteiro.', example: '/iana/rdap' },
    ],
  },
  {
    slug: 'ripe/asnames',
    path: '/ripe/asnames',
    name: 'Nomes de AS',
    group: 'rir',
    menuDesc: 'Nome e país de todos os ASNs alocados',
    summary: 'O nome e o país de todos os ASNs alocados, de todos os RIRs, a partir do asn.txt publicado pelo RIPE NCC.',
    about: [
      'O arquivo asn.txt do RIPE NCC traz uma linha por ASN alocado no mundo (cerca de 122 mil), com a descrição publicada — em geral o handle, o nome da organização e o país.',
      'A API devolve a linha de cada ASN com os campos derivados (handle, nome e país), lista os ASNs de um país, procura pelo handle e faz busca por texto na descrição.',
    ],
    data: ['Descrição de cada ASN, como publicada', 'Handle, nome e país derivados', 'Listas por país e por handle', 'Busca por texto'],
    source: 'https://ftp.ripe.net/ripe/asnames/asn.txt',
    routes: [
      { path: '/ripe/asnames/asn/{asn}', desc: 'Descrição, handle, nome e país do ASN.', example: '/ripe/asnames/asn/15169' },
      { path: '/ripe/asnames/country/{cc}', desc: 'Todos os ASNs de um país (código de duas letras).', example: '/ripe/asnames/country/BR' },
      { path: '/ripe/asnames/handle/{handle}', desc: 'Os ASNs com esse handle (o handle não é único).', example: '/ripe/asnames/handle/google' },
      { path: '/ripe/asnames/search?q={texto}', desc: 'Busca na descrição: termo de 3 a 100 caracteres, até 100 resultados.', example: '/ripe/asnames/search?q=google' },
    ],
  },
  {
    slug: 'cgibr',
    path: '/cgibr',
    name: 'NIC.br',
    group: 'rir',
    menuDesc: 'ASNs e blocos IP de titulares brasileiros, com o CNPJ',
    summary: 'Os ASNs e blocos IPv4/IPv6 de titulares brasileiros publicados pelo NIC.br (registro.br), ligados ao CNPJ do titular.',
    about: [
      'O NIC.br, pelo registro.br, publica todo dia útil o arquivo nicbr-asn-blk: uma linha por ASN de titular brasileiro, com o nome e o documento do titular e os blocos IPv4 e IPv6 dele.',
      'Diferente dos arquivos dos RIRs, ele liga ASNs e blocos ao CNPJ. A API responde a quem pertence um ASN, um IP ou um prefixo e o que cada documento tem.',
    ],
    data: ['ASNs de titulares brasileiros', 'Blocos IPv4 e IPv6 de cada ASN', 'Nome e documento (CNPJ) do titular'],
    source: 'https://ftp.registro.br/pub/numeracao/origin/nicbr-asn-blk-latest.txt',
    routes: [
      { path: '/cgibr/asn/{asn}', desc: 'O titular do ASN e os blocos dele.', example: '/cgibr/asn/61605' },
      { path: '/cgibr/ip/{ip}', desc: 'O bloco que contém o endereço e o seu titular.', example: '/cgibr/ip/179.63.178.0' },
      { path: '/cgibr/prefix/{ip}/{len}', desc: 'O bloco que contém o prefixo e o seu titular.', example: '/cgibr/prefix/179.63.179.0/24' },
      { path: '/cgibr/document/{doc}', desc: 'Os ASNs e blocos de um documento (CNPJ).', example: '/cgibr/document/37927622000170' },
      { path: '/cgibr/asns', desc: 'A lista de todos os ASNs do arquivo.', example: '/cgibr/asns' },
    ],
  },
  {
    slug: 'roothints',
    path: '/roothints',
    name: 'Root hints',
    group: 'dns',
    menuDesc: 'Os 13 servidores raiz do DNS',
    summary: 'O nome e os endereços IPv4 e IPv6 dos 13 servidores raiz do DNS (a a m.root-servers.net), do named.root publicado pela InterNIC.',
    about: [
      'O named.root traz os root hints: o que um resolvedor DNS usa para achar a raiz na partida. O conteúdo muda poucas vezes por ano, mas o arquivo é regravado a cada publicação da zona raiz.',
      'A API lista os servidores raiz com os endereços e os TTLs, e responde um servidor pela letra ou pelo nome. Cada resposta traz a data e o serial da zona raiz do arquivo carregado.',
    ],
    data: ['Os 13 servidores raiz', 'Endereços IPv4 e IPv6 e TTLs', 'Data e serial da zona raiz do arquivo'],
    source: 'https://www.internic.net/domain/named.root',
    routes: [
      { path: '/roothints/servers', desc: 'Todos os servidores raiz.', example: '/roothints/servers' },
      { path: '/roothints/server/{server}', desc: 'Um servidor, pela letra (k) ou pelo nome (k.root-servers.net).', example: '/roothints/server/a' },
    ],
  },
  {
    slug: 'rootzone',
    path: '/rootzone',
    name: 'Zona raiz',
    group: 'dns',
    menuDesc: 'TLDs delegados, servidores de nome, glue e DS',
    summary: 'A zona raiz do DNS inteira, do root.zone publicado pela InterNIC: os TLDs delegados com seus servidores de nome, o glue e os registros DS.',
    about: [
      'O root.zone tem o SOA (com o serial da versão), os NS e as chaves da raiz e, para cada um dos cerca de 1.400 TLDs delegados, os servidores de nome, o glue A/AAAA e os DS — a cadeia DNSSEC a partir da raiz. Sai uma versão nova cerca de duas vezes por dia.',
      'A API lista os TLDs e responde a delegação de cada um. O TLD pode vir em ASCII (xn--p1ai) ou em Unicode (рф); toda resposta traz o serial da zona.',
    ],
    data: ['TLDs delegados (ASCII e Unicode)', 'Servidores de nome e glue A/AAAA', 'Registros DS', 'Serial e SOA da zona'],
    source: 'https://www.internic.net/domain/root.zone',
    routes: [
      { path: '/rootzone/tlds', desc: 'Todos os TLDs delegados.', example: '/rootzone/tlds' },
      { path: '/rootzone/tld/{tld}', desc: 'A delegação de um TLD: NS, glue e DS.', example: '/rootzone/tld/br' },
    ],
  },
  {
    slug: 'rootanchors',
    path: '/rootanchors',
    name: 'Âncoras DNSSEC',
    group: 'dns',
    menuDesc: 'Âncoras de confiança DNSSEC da raiz (KSKs)',
    summary: 'As âncoras de confiança DNSSEC da zona raiz — as KSKs — do root-anchors.xml publicado pela IANA.',
    about: [
      'O root-anchors.xml traz cada chave de assinatura de chave (KSK) da raiz com o registro DS (key tag, algoritmo, tipo e valor do digest), a validade e, nas mais novas, o DNSKEY. Muda só nas etapas de uma rolagem de KSK, e a IANA mantém no arquivo as chaves aposentadas.',
      'A API lista as chaves e responde cada uma pela key tag, com o DS e o DNSKEY em texto, no formato de arquivo de zona que um resolvedor aceita como âncora.',
    ],
    data: ['KSKs da raiz, inclusive as aposentadas', 'DS e DNSKEY em texto', 'Validade de cada chave'],
    source: 'https://data.iana.org/root-anchors/root-anchors.xml',
    routes: [
      { path: '/rootanchors/keys', desc: 'Todas as chaves do arquivo.', example: '/rootanchors/keys' },
      { path: '/rootanchors/key/{key_tag}', desc: 'Uma chave pela key tag.', example: '/rootanchors/key/20326' },
    ],
  },
  {
    slug: 'anatel/pst',
    path: '/anatel/pst',
    name: 'Anatel — prestadoras',
    group: 'dns',
    menuDesc: 'Prestadoras de telecomunicações por CNPJ (SCM, STFC, SMP…)',
    summary: 'As prestadoras de serviços de telecomunicações da Anatel, por CNPJ: a outorga e cada serviço notificado (SCM, STFC, SMP, SeAC…), com o número Fistel e a data.',
    about: [
      'A Anatel publica, nos dados abertos de outorga e licenciamento, um arquivo com uma linha por serviço notificado de cada prestadora. O BadBlock guarda só as pessoas jurídicas (CNPJ) — as linhas de pessoa física são ignoradas.',
      'A API responde se um CNPJ é uma prestadora autorizada e de quê — por exemplo, se tem SCM (045), a licença de provedor de internet —, lista os códigos de serviço, as prestadoras de cada serviço (com filtro por UF) e busca prestadoras pelo nome.',
    ],
    data: ['Prestadoras pessoa jurídica, por CNPJ', 'Outorgas e serviços notificados, com Fistel e data', 'Prestadoras por código de serviço e UF', 'Busca pelo nome'],
    source: 'https://www.anatel.gov.br/dadosabertos/paineis_de_dados/outorga_e_licenciamento/prestadoras_servicos_telecomunicacoes.zip',
    routes: [
      { path: '/anatel/pst/provider/{cnpj}', desc: 'A prestadora, as outorgas e os serviços de um CNPJ (com ou sem pontuação).', example: '/anatel/pst/provider/02558157000162' },
      { path: '/anatel/pst/services', desc: 'Os códigos de serviço, com o nome e a quantidade de prestadoras.', example: '/anatel/pst/services' },
      { path: '/anatel/pst/service/{code}?state={UF}', desc: 'As prestadoras de um serviço; state filtra por UF.', example: '/anatel/pst/service/045?state=RR' },
      { path: '/anatel/pst/search?q={texto}&limit={N}', desc: 'Busca prestadoras pelo nome (até 100 por padrão).', example: '/anatel/pst/search?q=telefonica' },
    ],
  },
];

export const API_GROUPS = [
  { id: 'rir', label: 'Registros de internet' },
  { id: 'dns', label: 'DNS e telecomunicações' },
];

export const apiHref = (api) => `/apis/${api.slug}`;

export function findApi(slug) {
  return APIS.find((a) => a.slug === slug) ?? null;
}
