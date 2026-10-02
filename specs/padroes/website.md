# Padrão de todo site (`website-<site>`)

Os sites são as páginas públicas do BadBlock para pessoas (as APIs servem
máquinas). Cada pasta em `websites/` é um site, e cada site é um app com
imagem própria, publicado sozinho — como os coletores e as APIs, mas sem
banco, cache nem Go.

| Site | Pasta | Hosts servidos |
|---|---|---|
| `www` | `websites/www/` | `badblock.net.br` e `www.badblock.net.br` |

## Nomes

O nome do site segue a regra do nome de fonte: minúsculo, sem hífen
(`^[a-z0-9]+$`). É também o rótulo DNS abaixo de `WEBSITE_DOMAIN`, salvo o
`www`, que responde também pelo domínio puro.

| Coisa | Forma | Exemplo (`www`) |
|---|---|---|
| App | `website-<site>` | `website-www` |
| Pasta | `websites/<site>/` | `websites/www/` |
| Imagem | `tmsoftbrasil/badblock-website-<site>` | `tmsoftbrasil/badblock-website-www` |
| Container | `badblock-website-<site>` | `badblock-website-www` |
| Serviço e projeto compose | serviço `website-<site>`, projeto `badblock-website-<site>` | |
| Tag de release | `website-<site>/vX.Y.Z` | `website-www/v0.1.0` |
| Variáveis no `.env` | `WEBSITE_<SITE>_*` (e `WEBSITE_DOMAIN`, comum) | `WEBSITE_WWW_HOST_PORT` |
| Porta no loopback | `82NN`, uma por site | `8201` |
| Traefik | router e serviço `badblock-website-<site>` | |
| Host | `<site>.${WEBSITE_DOMAIN}`; o `www` também `${WEBSITE_DOMAIN}` | `www.badblock.net.br`, `badblock.net.br` |

## Código

Vite + React 18 + Tailwind CSS 3, em JavaScript (JSX, ESM; sem TypeScript).
Outras dependências de runtime só entram empacotadas no bundle (nada de
CDN); hoje o `www` usa `@xyflow/react` no quadro de fluxo da página inicial.
O site é uma SPA renderizada no navegador: o HTML de entrada só tem o
`<div id="root">` e o script do bundle, e é o mesmo em todas as páginas — o
`src/routes.js` escolhe a página pelo caminho (`location.pathname`), e os
links entre páginas são `<a href>` comuns (cada um carrega o HTML de novo).
O texto do site é em português (PT-BR), com `lang="pt-BR"`. O build (`vite build`) gera
`dist/` — HTML, CSS, JS e fontes com hash no nome —, servido na raiz do
host (`/`), sem servidor Node em
produção. Cada site tem o próprio `package.json` e o `package-lock.json`
versionado.

| Arquivo | Conteúdo |
|---|---|
| `index.src.html` | entrada do Vite (o build mantém o nome: `dist/index.src.html`); título, meta e o script que escolhe o tema claro/escuro antes da pintura |
| `src/main.jsx` | entry (`createRoot`) |
| `src/<Pagina>Page.jsx` | monta uma página a partir das seções e componentes (no `www`: `HomePage`, `ApisPage`, `ApiPage`, `NotFoundPage`) |
| `src/routes.js` | caminho → página e título (`document.title`); a lista de páginas segue a [tabela de rotas](#rotas) |
| `src/sections/`, `src/components/` | seções da página e componentes (navbar, rodapé, cards, ícones SVG inline) |
| `src/content/` | **textos e listas** (seções, cards, menus): o lugar de editar o conteúdo |
| `src/styles/tokens.css` | tokens do tema (CSS variables), `@font-face` e utilitários |
| `src/README.md` | mapa do código: onde mudar o quê |
| `fonts/`, `_ext/` | fontes servidas localmente (`_ext/` guarda os CSS e `woff2` do Google Fonts copiados); o Vite as empacota em `dist/assets/` |
| `landing-2/` | imagens (SVG, JPEG, PNG) citadas por caminho absoluto em `src/`; o Vite não as empacota, a imagem as copia como estão. A marca do BadBlock (o globo da navbar e do rodapé) fica em `landing-2/brand/` |
| `vite.config.js` | `base: '/'`, `outDir: 'dist'`, `rollupOptions.input: 'index.src.html'`, `publicDir: false`; no servidor de desenvolvimento, um middleware entrega o `index.src.html` nas rotas de página, como o `url.rewrite` do lighttpd |
| `tailwind.config.js`, `postcss.config.js` | Tailwind (`darkMode: 'class'`, breakpoint `xs` de 500px, `maxWidth.container` de 1160px, cores `gray`/`pink`/`green` e fundos ligados aos tokens de `tokens.css`, que mudam com o tema) e autoprefixer |
| `package.json`, `package-lock.json` | dependências; scripts `dev` (`vite`), `build` (`vite build`), `serve` (lighttpd local) e `test` |
| `tests/run.mjs` | teste do build ([abaixo](#testes)) |
| `lighttpd.docker.conf` | o servidor da imagem ([abaixo](#servidor)) |
| `lighttpd.conf` | o mesmo servidor para pré-visualizar o build na máquina (`npm run serve`, porta 8090, raiz em `dist/` e `landing-2/` por `alias.url`) |
| `Dockerfile`, `.dockerignore` | [abaixo](#imagem) |
| `docker-compose.yml`, `.env.example` | sobe o site sozinho a partir da pasta |
| `Makefile` | [abaixo](#makefile) |
| `README.md` | curto: o que é, comandos, link para esta spec e para o `src/README.md` |

Fora do git (`.gitignore` da raiz), nada que a compilação ou o servidor
local produzem: `node_modules/`, `dist/`, `.vite/`, `.next/`,
`*.tsbuildinfo` e os logs do lighttpd local (`*.log`).

Sem analytics, sem tracker, sem requisição a terceiros: fontes e imagens
saem da própria imagem (nada de Google Fonts ou CDN em tempo de execução).
Formulário que precise de backend chama uma API do BadBlock, nunca um
serviço externo. Conteúdo, marcas, logos e fotos são do BadBlock ou têm
licença que permite o uso: o repositório é público.

## Rotas

| Caminho | Resposta |
|---|---|
| `/` | `index.src.html` (a página inicial) |
| `/apis`, `/apis/<slug>` | `index.src.html` (a lista das APIs e a página de cada uma); `<slug>` é o caminho base da API sem a barra inicial (`lacnic`, `anatel/pst`), só os da lista de `src/content/apis.js`; aceita a barra final |
| `/assets/*` | bundle JS e CSS e fontes, com hash no nome |
| `/landing-2/*` | imagens |
| `/healthz` | `200 ok` |
| outro | `404` (não há fallback genérico de SPA: só as rotas de página acima entregam o `index.src.html`, e um slug fora da lista é 404) |

## Servidor

lighttpd (pacote do Alpine) com `lighttpd.docker.conf`, porta 8080, usuário
não-root:

- `server.document-root` com o conteúdo de `dist/` na raiz e `landing-2/`
  ao lado; `index-file.names` com `index.src.html`; `url.rewrite-once`
  (`mod_rewrite`) das rotas de página (`/apis` e `/apis/<slug>`, com os slugs
  escritos na regra e a query string aceita) para `/index.src.html`.
- `mimetype.assign` explícito (HTML, CSS e JS com `charset=utf-8`, `woff2`,
  `svg`, imagens).
- `/assets/`: `Cache-Control: public, max-age=31536000, immutable`
  (nomes com hash); o resto (HTML e `landing-2/`): `no-cache` (revalida com
  ETag/Last-Modified).
- `mod_deflate` para texto; `server.tag = ""`; cabeçalhos
  `X-Content-Type-Options: nosniff`, `Referrer-Policy:
  strict-origin-when-cross-origin`, `X-Frame-Options: DENY` (`mod_setenv`).
- `GET /healthz` → `200 ok`, sem log (healthcheck).
- Log de acesso em JSON na saída padrão (`accesslog.escaping = "json"`) e
  log de erro na saída de erro, com o IP real do cliente vindo de
  `X-Forwarded-For` (`mod_extforward`, confiando só nas redes privadas, como
  o `TRUSTED_PROXIES` das APIs).
- Só os módulos usados (sem `mod_dirlisting`: sem listagem de diretório).

## Imagem

```dockerfile
# syntax=docker/dockerfile:1
#
# website-<site>: site estático (Vite + React) servido por lighttpd sem root.
# O build roda na plataforma de quem compila (o dist/ não depende de
# arquitetura); só a imagem final é multi-arch.

FROM --platform=$BUILDPLATFORM node:24-alpine AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY . .
RUN npm run build

FROM alpine:3.22
ARG VERSION=dev
LABEL org.opencontainers.image.title="badblock-website-<site>" ...
RUN apk add --no-cache lighttpd
COPY lighttpd.docker.conf /etc/lighttpd/lighttpd.conf
COPY --from=build /src/dist/ /var/www/localhost/htdocs/
COPY landing-2 /var/www/localhost/htdocs/landing-2
USER lighttpd
EXPOSE 8080
HEALTHCHECK ... CMD ["wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/healthz"]
CMD ["lighttpd", "-D", "-f", "/etc/lighttpd/lighttpd.conf"]
```

O `dist/` sai do estágio de build, nunca da máquina de quem compila: a
imagem é reproduzível a partir do git. `.dockerignore`: `node_modules`,
`dist`, `.next`, `.vite`, `.src-work`, `*.log`, `.env`, `.env.*`,
`README.md`, `Makefile`, `docker-compose.yml`, `tests`, `.DS_Store`.

## Compose e Traefik

`docker-compose.yml` do site (projeto `badblock-website-<site>`): imagem
`${DOCKERHUB_NAMESPACE:-tmsoftbrasil}/badblock-website-<site>:${WEBSITE_<SITE>_TAG:-latest}`
com `build: .`, `restart: unless-stopped`, porta
`127.0.0.1:${WEBSITE_<SITE>_HOST_PORT:-82NN}:8080`, só a rede do Traefik (o
site não fala com o banco nem com o cache), log `json-file` 10m × 3, e as
labels:

```yaml
    labels:
      traefik.enable: "true"
      traefik.docker.network: ${TRAEFIK_NETWORK:-network_public}
      traefik.http.services.badblock-website-<site>.loadbalancer.server.port: "8080"
      traefik.http.routers.badblock-website-<site>.rule: Host(`<site>.${WEBSITE_DOMAIN:-badblock.net.br}`)
      traefik.http.routers.badblock-website-<site>.entrypoints: ${TRAEFIK_ENTRYPOINT:-websecure}
      traefik.http.routers.badblock-website-<site>.tls: "true"
      traefik.http.routers.badblock-website-<site>.tls.certresolver: ${TRAEFIK_CERTRESOLVER:-letsencrypt}
      traefik.http.routers.badblock-website-<site>.service: badblock-website-<site>
```

O `www` troca a regra por
``Host(`${WEBSITE_DOMAIN:-badblock.net.br}`) || Host(`www.${WEBSITE_DOMAIN:-badblock.net.br}`)``:
os dois hosts servem o mesmo conteúdo, com um certificado para cada nome. Os
registros DNS (A/AAAA dos dois nomes para o servidor) são pré-requisito para
o Let's Encrypt emitir.

Sem rate limit: o conteúdo é estático e barato. `.env.example` do site:
`WEBSITE_<SITE>_TAG`, `WEBSITE_<SITE>_HOST_PORT`, `WEBSITE_DOMAIN`,
`TRAEFIK_NETWORK`, `TRAEFIK_ENTRYPOINT`, `TRAEFIK_CERTRESOLVER`.

## Testes

`tests/run.mjs`, em Node puro (sem dependência), roda depois do build: sobe
um servidor estático com `dist/` na raiz e `landing-2/` ao lado, com as
rotas acima, e confere que `/` responde 200, que cada asset citado pelo
`index.src.html` responde 200, que cada rota de página entrega o
`index.src.html` e um slug fora da lista dá 404, que os slugs da regra de
`url.rewrite` dos dois `lighttpd*.conf` são os de `src/content/apis.js`, que
cada imagem `/landing-2/...` citada em `src/` existe, e os textos esperados
(e os proibidos) no bundle JS (a página é renderizada no navegador, então os
textos não estão no HTML). Textos esperados mudam junto com o conteúdo.

## Makefile

`APP := website-<site>`, `ROOT := ../..`, `VERSION` (da tag
`website-<site>/vX.Y.Z` mais recente, ou `dev`), `IMAGE`, `PLATFORMS`,
`PORT := $${WEBSITE_<SITE>_HOST_PORT:-82NN}`,
`COMPOSE := docker compose --env-file $(ROOT)/.env`.

| Alvo | Faz |
|---|---|
| `help` | lista os alvos (padrão) |
| `deps` | `npm ci` |
| `dev` | `npm run dev` (servidor do Vite com HMR, http://localhost:5173/index.src.html) |
| `build` | `npm run build` → `dist/` |
| `serve` | `build` + `npm run serve` (lighttpd local, http://localhost:8090) |
| `test` | `build` + `npm test` (`node tests/run.mjs`) |
| `lint` | `build`: sem TypeScript, o build do Vite é a checagem estática (import quebrado e JSX inválido falham) |
| `image`, `push` | como nos apps Go ([../plataforma/docker.md](../plataforma/docker.md#makefile-dos-apps)), com versão, commit e data só nos labels OCI (o site não tem binário) |
| `up`, `down`, `logs` | o compose do site com o `.env` da raiz |
| `smoke` | `curl -fsS` pela porta do loopback em `/healthz`, `/`, `/apis` e numa rota que não existe (espera 404) |

## Pendências do `www`

O `www` ainda não cumpre todo o padrão acima:

- `docker-compose.yml`: serviço `web`, porta `8090:8080` em todas as
  interfaces, sem imagem nomeada, sem rede do Traefik, sem labels e sem
  limite de log.
- `Dockerfile`: já compila o `dist/` no estágio de build, mas a imagem
  final usa `alpine:3.20` (fora de suporte), roda como root e não tem
  `HEALTHCHECK` nem labels OCI.
- `lighttpd.docker.conf`: sem `/healthz`, cabeçalhos de segurança e de
  cache, compressão, `server.tag`, log de acesso JSON nem `mod_extforward`;
  traz a chave `dir-listing.activate` sem o módulo (o lighttpd avisa e
  ignora).
- Faltam `Makefile` e `README.md`, e o `make test`/`make lint` da raiz
  falham no site (o `npm test` já roda o `tests/run.mjs`).
- Conteúdo: a marca (`Logo.jsx`), a navbar (só a marca e o tema), o
  rodapé (`Footer.jsx`), o quadro de fluxo e as páginas `/apis`
  (`src/content/apis.js`) já descrevem o BadBlock. A página inicial tem só o
  Hero e o quadro de fluxo; a versão anterior, com as seções de
  `src/content/features.js` (textos e imagens de `landing-2/features/` que
  ainda não descrevem o BadBlock), está guardada em `src/drafts/draft01/`,
  fora das rotas e do bundle.
