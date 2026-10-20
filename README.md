# Pinglume

Ferramenta local para investigar falhas intermitentes de internet durante chamadas: registra medições pequenas ao longo do tempo, marca quando houve medição e quando não houve, e guia comparações antes/depois.

**Estado:** em desenvolvimento, Nível 1. Ainda não há endpoints públicos hospedados; o agente só mede o `probe-server` local (testbed). Desenho completo em [docs/00-desenho-inicial.md](docs/00-desenho-inicial.md).

## O que existe hoje

| Fatia | Situação |
| --- | --- |
| F0 repositório e toolchain | feito |
| F1 sonda TCP → SQLite → `pinglume status` | feito e testado |
| F2 cobertura: heartbeat, agente parado, suspensão, pausa | feito; suspensão testada só com relógio simulado |
| F3 em diante | ver backlog no documento de desenho |

## Rodar localmente

Requisitos: Go 1.27 (versões em [docs/versoes.md](docs/versoes.md)).

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

Resumo das medições:

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
- Pausa/retomada usam sinais Unix entre a CLI e o agente; a API local autenticada (F3) vai substituir esse canal.
- Nenhum número de desempenho foi publicado; medições de recursos são a fatia F11.
