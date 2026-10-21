# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/); versões seguem [SemVer](https://semver.org/lang/pt-BR/). Enquanto a versão for `0.y.z`, API local, schema e interface podem mudar.

## [Não publicado]

## [0.1.0] - 2026-10-21

Primeira versão. Nível 1 ainda incompleto: só existe a sonda TCP, e ela mede apenas o `probe-server` local (os endpoints próprios ainda não estão hospedados).

### Adicionado

- Agente local em Go, binário único, com SQLite embutido (driver pure-Go) e migrações versionadas.
- Sonda TCP com status por protocolo (`ok`, `refused`, `timeout`, `cancelled`…); nenhum status afirma perda de pacotes IP.
- Definições de sonda imutáveis: mudar um parâmetro cria uma nova versão, que nunca se mistura com a anterior.
- Agendador com concorrência limitada, jitter e sequência contínua entre reinícios; instância única por diretório de dados.
- Modelo de cobertura: heartbeat, agente parado, encerramento inesperado, suspensão do computador (heurística civil × monotônico), relógio alterado e pausa persistente.
- API local só em `127.0.0.1`, com token `0600` para a CLI, sessão de navegador por código de uso único, validação de `Host`/`Origin`/`Sec-Fetch-Site` e sem CORS.
- CLI: `run`, `open`, `status`, `pause`, `resume`, `version`.
- Dashboard React/TypeScript embutido no binário: cobertura, períodos sem medição, resumo por sonda e estados sem sessão, offline e erro.
- Linha do tempo com faixa de cobertura, pequenos múltiplos por sonda (mediana, p95 e falhas), lacunas hachuradas, zoom por arraste, navegação por teclado e tabela equivalente.
- `probe-server` mínimo com falha simulada para o testbed local.
- CI no GitHub Actions: testes do agente em macOS e Linux e do dashboard.

### Limites conhecidos

- Suspensão detectada só por heurística; não validada com o Mac dormindo de verdade e não distingue sono de relógio adiantado.
- Sem sondas DNS/HTTPS/UDP, incidentes, comparação guiada, exportação, retenção ou modo demo (fatias F5–F12).
- Nenhuma medição de recursos (CPU, memória, tráfego) publicada ainda.

[Não publicado]: https://github.com/pdrflp0/pinglume/compare/v0.1.0...develop
[0.1.0]: https://github.com/pdrflp0/pinglume/releases/tag/v0.1.0
