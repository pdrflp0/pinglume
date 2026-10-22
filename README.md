# Pinglume

Ferramenta local para investigar falhas intermitentes de internet durante chamadas: registra medições pequenas ao longo do tempo, marca quando houve medição e quando não houve, e guia comparações antes/depois.

**Estado:** em desenvolvimento, Nível 1. Ainda não há endpoints públicos hospedados; o agente só mede o `probe-server` local (testbed). Desenho completo em [docs/00-desenho-inicial.md](docs/00-desenho-inicial.md).

## O que existe hoje

| Fatia | Situação |
| --- | --- |
| F0 repositório e toolchain | feito |
| F1 sonda TCP → SQLite → `pinglume status` | feito e testado |
| F2 cobertura: heartbeat, agente parado, suspensão, pausa | feito; suspensão testada só com relógio simulado |
| F3 API local segura + dashboard servido pelo agente | feito e testado |
| F4 linha do tempo com lacunas, zoom, teclado e tabela equivalente | feito e testado |
| F5 sondas DNS e HTTPS, backoff e injeção de falhas no probe-server | feito e testado |
| F6 relatos ("travou agora") e análise `incident-v1` | feito e testado |
| F7 em diante | ver backlog no documento de desenho |

## Rodar localmente

Requisitos: Go 1.27 e Node 24+ (versões em [docs/versoes.md](docs/versoes.md)). `make build` compila o dashboard e o embute no binário.

```bash
make build
```

```bash
make test
```

Terminal 1 — endpoint local de sondas:

```bash
make dev-server
```

Terminal 2 — agente (Ctrl+C encerra):

```bash
make dev-agent
```

Abrir o dashboard (gera um link de uso único, válido por 60 s):

```bash
agent/bin/pinglume open --data-dir .dev-data
```

Resumo das medições no terminal (lê o banco; funciona com o agente parado):

```bash
agent/bin/pinglume status --data-dir .dev-data
```

Relatar que a chamada travou (também há o botão **Travou agora** no dashboard):

```bash
agent/bin/pinglume mark --data-dir .dev-data --symptom audio_cut --note "reunião"
```

Ver os relatos e a análise de um deles:

```bash
agent/bin/pinglume incidents --data-dir .dev-data
```

A análise (regra `incident-v1`) olha as medições de 5 min antes a 1 min depois do relato, compara com as 2 h anteriores e só fica definitiva quando essa janela fecha. Ela sempre lista outras explicações e um próximo teste, e nunca apresenta coincidência no tempo como causa.

Pausar e retomar (a pausa persiste se o agente reiniciar):

```bash
agent/bin/pinglume pause --data-dir .dev-data
```

```bash
agent/bin/pinglume resume --data-dir .dev-data
```

### Simular falhas no testbed

O `probe-server` tem uma API de administração só em loopback (`127.0.0.1:9900`). Exemplos:

```bash
curl -X PUT -d '{"dns":"drop"}' http://127.0.0.1:9900/faults
```

```bash
curl -X PUT -d '{"dns":"ok","listener":"closed"}' http://127.0.0.1:9900/faults
```

```bash
curl -X PUT -d '{"listener":"open","https":"ok","httpsDelay":"0s"}' http://127.0.0.1:9900/faults
```

| Campo | Valores |
| --- | --- |
| `dns` | `ok`, `drop` (não responde), `servfail`, `nxdomain` |
| `https` | `ok`, `error500`, `hang` (segura a resposta) |
| `httpsDelay` | duração, ex.: `300ms` |
| `listener` | `open`, `closed` (TCP e HTTPS recebem conexão recusada) |

O certificado autoassinado do testbed é criado em `testbed/tls/` na primeira execução (fora do git) e fixado pelo agente com `--dev-ca`.

## Desenvolvimento

Branches, versionamento (SemVer) e convenção de commits: [docs/fluxo-git.md](docs/fluxo-git.md). Histórico de versões: [CHANGELOG.md](CHANGELOG.md).

## Limites conhecidos

- No testbed, a sonda DNS consulta o servidor de teste diretamente (`--dev-dns`). Em produção ela usará o resolvedor do sistema; as duas são definições diferentes e nunca se comparam.
- Em backoff (até 8× o intervalo), a volta de uma sonda demora mais para aparecer: no ensaio, o DNS restaurado só foi medido ~22 s depois.
- Sonda UDP (perda e jitter) é a fatia F12.
- Os limiares da `incident-v1` (50% de cobertura, latência 2× a mediana e acima do p95 da base, 10 execuções ok de base) são escolhas iniciais, ainda não calibradas com uso real.
- O NetContext (Wi-Fi × cabo) ainda não é coletado; a linha de base compara com as 2 h anteriores mesmo que a rede tenha mudado.
- Alvos de desenvolvimento precisam ser loopback ou rede privada; o agente recusa outros.
- A detecção de suspensão compara relógio civil e monotônico. Ela não distingue sono de relógio adiantado manualmente (o motivo registrado diz isso) e ainda não foi validada com o Mac dormindo de verdade.
- Depois de um crash, a lacuna começa no último heartbeat (a cada 5 s): o instante exato da queda tem até 5 s de incerteza.
- A linha do tempo usa escala linear compartilhada entre as sondas: um pico grande achata as demais. Escala logarítmica fica para depois do uso real.
- Os trechos (baldes) têm tamanho período ÷ 300; em 7 dias, cada trecho cobre ~34 min e a mediana esconde picos curtos dentro dele.
- Eventos do sistema (início/parada do agente) já vêm da API, mas ainda não aparecem como marcas no gráfico.
- Sessões do navegador ficam em memória: reiniciar o agente exige `pinglume open` de novo.
- Em largura de celular, a tabela de sondas rola horizontalmente dentro do cartão.
- Nenhum número de desempenho foi publicado; medições de recursos são a fatia F11.

## Segurança da API local

- Escuta só em `127.0.0.1:47821` (configurável com `--port`).
- Token de 256 bits em `api-token` (permissão 0600; o agente recusa arquivo legível por outros). A CLI usa o token; o navegador nunca o recebe.
- O navegador entra por `pinglume open`: código de uso único (60 s) → cookie `HttpOnly; SameSite=Strict`.
- `Host` diferente do próprio loopback → 421 (defesa contra DNS rebinding).
- Pedidos de outra origem (`Origin`, `Sec-Fetch-Site`) → 403, inclusive leituras. Mudança de estado por cookie exige `Origin` igual ao do agente.
- Nenhum cabeçalho CORS. CSP restritiva, `frame-ancestors 'none'`.
- Nenhuma rota executa comando, altera rede ou aceita alvo de sonda.

Os testes em `agent/internal/api/server_test.go` cobrem cada item.
