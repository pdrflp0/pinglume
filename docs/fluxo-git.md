# Fluxo de branches e versionamento

## Branches (Git Flow)

| Branch | Para quê | Recebe merge de |
| --- | --- | --- |
| `main` | Só versões publicadas. Cada merge aqui é uma release com tag. | `release/*`, `hotfix/*` |
| `develop` | Integração do que vai para a próxima versão. | `feature/*`, `release/*`, `hotfix/*` |
| `feature/<nome>` | Uma fatia ou mudança isolada, criada a partir de `develop`. | — |
| `release/<x.y.z>` | Preparação da versão: número, CHANGELOG, ajustes finais. Sai de `develop`. | — |
| `hotfix/<x.y.z>` | Correção urgente sobre uma versão publicada. Sai de `main`. | — |

Merges entre essas branches usam `--no-ff`, para que cada feature e cada release apareçam como um bloco no histórico.

### Feature

```bash
git switch -c feature/f5-sondas-dns-https develop
```

Ao terminar (testes passando), merge em `develop` e apague a branch.

### Uma versão por fatia

Cada fatia do backlog (F1, F2, …) termina numa versão publicada, sem acumular fatias em `develop`:

1. a fatia é feita em `feature/<fatia>` e entra em `develop` com merge `--no-ff`;
2. em seguida sai a `release/x.y.0`, com a versão e o CHANGELOG detalhado;
3. a release entra em `main` com tag anotada `vX.Y.Z`;
4. `main`, `develop` e a tag são enviados ao GitHub e a Release é criada com as notas daquela versão;
5. `develop` volta para `x.(y+1).0-dev`.

As mensagens do commit de release e do merge em `main` listam o que foi adicionado, alterado, corrigido e removido, no mesmo formato do CHANGELOG.

### Release

1. `git switch -c release/0.2.0 develop`
2. Atualize a versão em `agent/cmd/pinglume/main.go` e `web/package.json`, e mova as entradas de "Não publicado" do `CHANGELOG.md` para a nova versão.
3. Merge em `main`, tag anotada `v0.2.0`, merge de volta em `develop`.
4. Em `develop`, volte a versão para `0.3.0-dev`.

### Hotfix

Igual à release, partindo de `main` e incrementando o PATCH (`v0.2.1`).

## Versionamento (SemVer)

`MAJOR.MINOR.PATCH`, seguindo [semver.org](https://semver.org/lang/pt-BR/).

- Enquanto a versão for `0.y.z`, o projeto está em desenvolvimento inicial: a API local, o schema e a interface ainda podem mudar sem aviso.
- MINOR sobe a cada fatia entregue; PATCH, para correções sobre uma versão publicada.
- Entre releases, `develop` carrega o sufixo `-dev` (ex.: `0.2.0-dev`), que aparece em `pinglume version` e no rodapé do dashboard.
- `1.0.0` só depois do Nível 1 completo e validado com uso real.

## Mensagens de commit (Conventional Commits)

`tipo(escopo): resumo no imperativo, em português`

| Tipo | Uso |
| --- | --- |
| `feat` | funcionalidade nova |
| `fix` | correção de defeito |
| `docs` | só documentação |
| `test` | só testes |
| `refactor` | mudança interna sem efeito externo |
| `ci` | pipeline de integração |
| `chore` | manutenção (versão, dependências, release) |

Escopos usados: `agent`, `api`, `dashboard`, `probe-server`, `store`, `release`.
