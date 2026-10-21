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
| F5 em diante | ver backlog no documento de desenho |

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

Pausar e retomar (a pausa persiste se o agente reiniciar):

```bash
agent/bin/pinglume pause --data-dir .dev-data
```

```bash
agent/bin/pinglume resume --data-dir .dev-data
```

Para simular uma falha, rode o servidor com `-fault close -fault-after 30s`: a escuta fecha e as sondas passam a registrar `refused`.

## Limites conhecidos

- Só a sonda TCP existe; DNS, HTTPS e UDP chegam em F5 e F12.
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
