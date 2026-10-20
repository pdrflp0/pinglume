# Versões fixadas

Registradas quando instaladas ou adicionadas. Atualizar esta tabela junto com a mudança.

| Componente | Versão | Onde está fixada | Data |
| --- | --- | --- | --- |
| Go | 1.27.3 (darwin/arm64, Homebrew) | `agent/go.mod`, `probe-server/go.mod` (`go 1.27.3`) | 20/10/2026 |
| modernc.org/sqlite (driver pure-Go, sem CGO) | v1.60.1 | `agent/go.mod` | 20/10/2026 |
| github.com/oklog/ulid/v2 | v2.1.2 | `agent/go.mod` | 20/10/2026 |
| sqlite3 CLI (só inspeção manual) | 3.51.0 (do sistema) | — | 20/10/2026 |
| Node.js | v26.11.1 (Current, não LTS) — fixar LTS em `.nvmrc` quando `web/` nascer (F3) | — | 20/10/2026 |
| Docker (só desenvolvimento/testbed) | 29.7.2 | — | 20/10/2026 |

Ambiente de referência: macOS 26.6.2 (25G83), Apple Silicon.
