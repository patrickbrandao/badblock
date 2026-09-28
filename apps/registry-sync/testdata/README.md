# Fixtures

`sources/<id>` é um recorte pequeno e real de cada fonte do catálogo, usado
pelos testes unitários, pelos de integração e pelo e2e (o registry-sync lê
este diretório com `SOURCES_DIR`).

- Arquivos delegated (`rir-*`): registros escolhidos (TMSoft, RNP, Google,
  Microsoft Brasil, Cogent Brasil, registros fora de CIDR, available e
  reserved), com cabeçalho, linhas `summary` e `.md5` recalculados.
- `nicbr`: as linhas de AS61613, AS1916, AS174, AS8075 e AS2635, só com blocos
  presentes no recorte da LACNIC (como nos dados reais).
- `asnames`: os nomes de todos os ASNs presentes nos recortes.
- IANA e bootstrap RDAP: arquivos inteiros (são pequenos).

Para regerar a partir das fontes baixadas (nomes originais dos arquivos, ver o
cabeçalho do script):

```bash
make fixtures SRC=/caminho/das/fontes
```
