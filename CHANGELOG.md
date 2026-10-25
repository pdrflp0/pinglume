# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/); versões seguem [SemVer](https://semver.org/lang/pt-BR/). Enquanto a versão for `0.y.z`, API local, schema e interface podem mudar.

## [Não publicado]

### Adicionado

- Sonda DNS: resolve um nome único por execução (`<nonce>.<zona>`) para evitar cache; NXDOMAIN/SERVFAIL viram `dns_error`, sem resposta vira `timeout`.
- Sonda HTTPS: conexão nova a cada execução, por IP (independe de DNS), certificado do testbed fixado, fases `connect`/`tls`/`ttfb` gravadas; status `tls_error` e `http_error` separados de falhas de rede.
- Backoff por sonda após 3 falhas seguidas (intervalo ×2 até ×8), registrado em eventos e exibido como tempo em backoff no resumo, na CLI e na linha do tempo.
- `probe-server`: HTTPS com certificado autoassinado persistido, DNS autoritativo mínimo para a zona de teste, limite por IP e API de falhas em loopback (`dns`, `https`, `httpsDelay`, `listener`).

- Relatos "travou agora" pelo dashboard (diálogo com sintoma, horário e observação) e pela CLI (`pinglume mark`, `pinglume incidents`), distintos de medições na interface e marcados na linha do tempo.
- Regra `incident-v1`: janela de 5 min antes a 1 min depois do relato, linha de base das 2 h anteriores, vereditos `aguardando_dados`, `sem_cobertura`, `degradacao_observada` e `sem_degradacao_detectavel`; cada frase traz as entradas que a sustentam, sempre com alternativas, próximo teste e limites. A interpretação fica gravada e imutável quando a janela fecha.
- Exclusão de relatos, com confirmação, apagando também a interpretação.
- Detecção do tipo de rede no macOS (rota padrão + `networksetup`, só leitura): interface e tipo (Wi-Fi, cabo, VPN) registrados no início e a cada mudança (`net_context`, `net_change`).
- Comparação guiada no dashboard: janela A, mudança declarada, janela B, com condições declaradas e detectadas; uma comparação ativa por vez, cancelável.
- Regra `compare-v1`: pré-condições por sonda (mesma definição, ≥ 10 min, ≥ 80% de cobertura), diferença B − A de mediana e de taxa de falha com intervalo de 95% por bootstrap em blocos (reprodutível por experimento), limiares práticos, vereditos `melhora`, `piora`, `sem_diferenca_detectavel`, `inconclusivo` e `nao_comparavel`, e fatores de confusão apontados. Resultado gravado ao fim.

### Alterado

- Rótulos de latência agora dizem o que cada tipo de sonda mede (handshake, resolução, requisição completa).
- Números nos textos das regras e do dashboard usam vírgula decimal.

### Corrigido

- Latências negativas (diferenças) eram exibidas como "-25000 µs" no dashboard.

### Removido

- Flag `-fault`/`-fault-after` do `probe-server`, substituída pela API de falhas.

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
