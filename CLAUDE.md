# BadBlock

As instruções deste repositório estão em [`AGENTS.md`](AGENTS.md) — leia antes
de mexer em qualquer coisa.

O que mais dá errado quando ignorado:

- **Todo SQL de schema mora em `database/postgresql/`** (bootstrap e
  migrations). Os apps só leem e escrevem dados; a registry-api só enxerga as
  views do schema `api`.
- **O repositório é público.** Nenhum segredo no git; valores reais só no `.env`.
- As decisões de desenho estão em [`docs/decisoes-fase-1.md`](docs/decisoes-fase-1.md).
