# Publicação: Traefik, imagens e produção

## Traefik

Produção tem um Traefik v3 já rodando no servidor, com `exposedByDefault`:
ele publica **todo** container. Por isso:

- Postgres, migrate, Valkey e coletores levam `traefik.enable: "false"`.
- Cada API publica o próprio caminho no host da API, pelas labels do seu
  `docker-compose.yml`:

```yaml
    labels:
      traefik.enable: "true"
      traefik.docker.network: ${TRAEFIK_NETWORK:-network_public}

      traefik.http.services.badblock-api-<fonte>.loadbalancer.server.port: "8080"

      traefik.http.routers.badblock-api-<fonte>.rule: Host(`${API_FQDN:-api.badblock.net.br}`) && (Path(`/<fonte>`) || PathPrefix(`/<fonte>/`))
      traefik.http.routers.badblock-api-<fonte>.entrypoints: ${TRAEFIK_ENTRYPOINT:-websecure}
      traefik.http.routers.badblock-api-<fonte>.tls: "true"
      traefik.http.routers.badblock-api-<fonte>.tls.certresolver: ${TRAEFIK_CERTRESOLVER:-letsencrypt}
      traefik.http.routers.badblock-api-<fonte>.service: badblock-api-<fonte>
      traefik.http.routers.badblock-api-<fonte>.middlewares: badblock-api-<fonte>-ratelimit

      traefik.http.middlewares.badblock-api-<fonte>-ratelimit.ratelimit.average: ${API_RATE_AVERAGE:-5}
      traefik.http.middlewares.badblock-api-<fonte>-ratelimit.ratelimit.burst: ${API_RATE_BURST:-20}
      traefik.http.middlewares.badblock-api-<fonte>-ratelimit.ratelimit.period: 1s
```

| Item | Valor (variável no `.env`) |
|---|---|
| Host | `API_FQDN` = `api.badblock.net.br` — o mesmo para todas as APIs, cada uma com o seu caminho |
| Rede | `TRAEFIK_NETWORK` = `network_public` |
| Entrypoint | `TRAEFIK_ENTRYPOINT` = `websecure` |
| Certificado | `TRAEFIK_CERTRESOLVER` = `letsencrypt` |
| Rate limit | por IP: `API_RATE_AVERAGE` = 5 req/s, `API_RATE_BURST` = 20, período 1 s |

Os sites publicam **hosts inteiros** (`badblock.net.br`, `www.badblock.net.br`,
`<site>.badblock.net.br`), sem rate limit, pelas labels descritas em
[../padroes/website.md](../padroes/website.md#compose-e-traefik); o domínio
vem de `WEBSITE_DOMAIN`.

O Traefik **não remove** o prefixo: a API recebe `/<fonte>/...` e responde
por ele (`BASE_PATH`). Sem Traefik (desenvolvimento), cada API fica no
loopback do host, na porta da fonte:
`curl http://127.0.0.1:<porta>/<fonte>/status`.

## Release das imagens

- Imagens no Docker Hub, conta `tmsoftbrasil`: `tmsoftbrasil/badblock-<app>`.
  Não há CI publicando: quem publica roda o script numa máquina autenticada
  (`docker login`).
- Versão = a do projeto, a última tag git **`vX.Y.Z`** (SemVer, sufixo
  opcional como `-rc.1`), igual para todos os apps no `release-images.sh`
  (`v0.0.1` → `:0.0.1`); `VERSION=X.Y.Z` no ambiente a troca. A tag da
  imagem não depende do commit nem de haver alteração não commitada.
- Cada imagem sai com duas tags: a versão e `latest`. Voltar atrás =
  `<APP>_TAG=<versão anterior>` no `.env` do servidor.

### `release-images.sh` (raiz, versionado)

```bash
./release-images.sh                 # todos os apps, dos fontes do disco
./release-images.sh api-cgibr       # só os nomes passados
PLATFORMS=linux/amd64 ./release-images.sh   # uma arquitetura só
VERSION=0.0.2 ./release-images.sh           # outra versão que a última tag
```

1. `APPS` lista os 27 apps (26 de fontes + `website-www`); nome desconhecido
   é erro. A pasta de cada um sai do nome: `collector-<fonte>` →
   `apps/<fonte>/collector` (numa fonte de dois níveis o `-` vira `/`:
   `collector-anatel-pst` → `apps/anatel/pst/collector`), `website-<site>` →
   `websites/<site>`.
2. Resolve a seleção **antes** da confirmação: sem nomes, todos os apps. A
   versão é `VERSION` ou, sem ela, a última tag `vX.Y.Z` do projeto
   (`git describe --tags --abbrev=0 --match 'v[0-9]*'`), sem o `v`; tem de
   ser SemVer, e sem nenhuma das duas o script recusa.
3. Exige `docker buildx` com um builder que conheça todas as `PLATFORMS`
   (padrão `linux/amd64,linux/arm64`), senão recusa antes do primeiro build.
4. O build sai dos arquivos do disco; `BUILD_DATE` em UTC. Nem a tag nem os
   labels da imagem levam o commit.
5. **Uma confirmação** (`[s/N]`) mostrando versão, plataformas e imagens.
6. Laço de build: `docker buildx build --platform ... --build-arg
   VERSION/BUILD_DATE --label org.opencontainers.image.version
   -t <imagem>:<versão> --push apps/<fonte>/<tipo>` — só a tag de versão.
7. Só depois de **todas** publicadas, a `latest` anda:
   `docker buildx imagetools create -t <imagem>:latest <imagem>:<versão>`
   (cópia do manifesto no registry, sem rebuild).
8. Falha no meio: um `trap` diz em que estado o Hub ficou (quais versões
   subiram, quais `latest` faltam mover) e o que rodar. Ctrl-C passa pelo
   trap.

Testes não rodam no script: rode antes os de
[../processos/testes.md](../processos/testes.md#antes-de-um-pr). Cada app
também publica sozinho com `make -C apps/<fonte>/<tipo> push` (site:
`make -C websites/<site> push`).

## Scripts do mantenedor

Na raiz, **fora do git** — são privados e locais do desenvolvedor (valores
do ambiente dele); todo `run-*.sh` fica no `.gitignore` e nenhum entra em
commit:

- `run-build.sh` — `docker build` de cada imagem na arquitetura local, tag
  `yyyy-mm-dd-HHMM` e `latest`. `VERSION` e `BUILD_DATE` são valores fixos,
  editados à mão só quando sai uma versão nova: como entram no `go build`
  por `--build-arg`, valores que mudam a cada execução derrubariam o cache.
- `run-buildx.sh` — o mesmo conteúdo e a mesma sintaxe do `run-build.sh`, com
  `docker buildx build --platform linux/amd64,linux/arm64 --load` (o `--load`
  multi-arch exige o image store containerd do Docker).
- `run-local.sh` (tudo, ou um serviço), `run-rebuild.sh`, `run-commit.sh`
  (branch de release, commit, merge e tag) e `run-server.sh` (`docker run`
  sem compose, com as labels do Traefik).
- `run-hub.sh` — `docker push` de todas as imagens na tag `latest`; acompanha
  o `run-build.sh` (mesmas imagens, mesma ordem).
- `destroy.sh` e `sync.sh` (rsync para o
  servidor de testes).

Regras:

- `run-server.sh` **não se edita sem pedido explícito do usuário**. Um app
  novo precisa de um bloco nele — quem integra descreve o bloco e o
  mantenedor aplica.
- `run-build.sh` e `run-buildx.sh` **acompanham o `docker-compose.yml` da
  raiz**: toda mudança que inclui, remove ou renomeia um app no compose
  atualiza o bloco dele nos dois scripts (`echo "Construindo imagem …"`, o
  build, linha em branco entre imagens), na mesma ordem; o `run-hub.sh`
  ganha ou perde o `docker push` da imagem.
- Nenhum outro arquivo do projeto depende desses scripts.

## Produção

Um servidor do mantenedor com Docker e o Traefik acima, rede
`network_public`, os containers `badblock-*` e o `.env` com os valores
reais. Endereços, acessos e senhas ficam fora do repositório (que é público).
O que roda lá é o mesmo stack: Postgres, Valkey, os coletores e as APIs, cada
um pela sua imagem.
