@AGENTS.md

## Claude Code

- **Sub-agentes**: `.claude/agents/<app>.md`, um por app (o tipo do agente é o
  nome do app, ex.: `collector-cgibr`). Delegue a eles o trabalho de um app;
  o que cruza apps (padrões, integração na raiz, specs de projeto e
  plataforma) fica com a sessão principal, que distribui o trabalho de cada
  app ao sub-agente dele. Os sub-agentes também recebem este arquivo.
- **Skills** do MCP `badblock-dev` (`.mcp.json`, fora do git):
  `postgres-table-style`, `postgres-uuidv7`, `postgres-url-standalone` e
  `redis-url-standalone`. Use-as ao mexer em schema e em conexões.
