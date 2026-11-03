# Medição de recursos do agente

Medição real no testbed local, gerada por `testbed/measure/measure.sh` e `testbed/measure/summarize` a partir dos arquivos brutos da pasta `2026-11-01-24h` (`env.txt`, `samples.csv`, `counts.csv`, `coverage.csv`, `suspended.csv`, `pmset.csv`). Nada aqui é estimativa de desenvolvimento, exceto o que está marcado como modelo. Para regenerar: `go -C testbed/measure run ./summarize <pasta>`.

## Ambiente

| Item | Valor |
| --- | --- |
| inicio | 2026-11-01T22:33:54Z |
| fim | 2026-11-02T22:34:50Z |
| macos | 26.6.2 (25G83) |
| cpu | Apple M2, 8 núcleos |
| memoria_gb | 16 |
| go | go1.27.3 |
| agente | 0.6.0-dev |
| commit | 5205538 |
| sondas | 3 endpoints × (tcp a cada 10 s, dns a cada 30 s, https a cada 60 s), intervalos padrão |
| tempo medindo, segundo o próprio agente | 5,23 h |
| computador suspenso, segundo o próprio agente | 18,78 h em 97 lacunas |
| amostras de 1 min do roteiro | 291 (4,41 h entre amostras seguidas; 19,58 h em intervalos > 2 min excluídos) |

As taxas por hora valem para o computador acordado e o agente medindo. Tempo suspenso não entra no denominador; a projeção de disco supõe operação contínua.

## Resultados

| Recurso | Medido |
| --- | --- |
| CPU, média (tempo de CPU total ÷ tempo medindo) | 0,069% de um núcleo |
| CPU, p95 dos minutos | 0,091% de um núcleo |
| CPU, pior minuto | 0,133% de um núcleo |
| Memória residente, média | 19,30 MB |
| Memória residente, máxima | 23,70 MB |
| Tráfego de dados no socket (nettop), entrada | 408,6 KB/h |
| Tráfego de dados no socket (nettop), saída | 328,6 KB/h |
| Execuções de sonda | 8476 em 5,23 h medindo (1619/h; esperado com os intervalos padrão: 1620/h) |
| Banco de dados, crescimento por execução | 224 B |
| Banco de dados, crescimento por hora medindo | 354,6 KB |
| Banco de dados, tamanho final | 2,00 MB |

## Tráfego total com cabeçalhos

O `nettop` conta só os dados no socket. Os cabeçalhos IPv4 e TCP/UDP vêm de um modelo: pacotes por execução × tamanho do cabeçalho × execuções gravadas no banco.

| Sonda | Execuções | Pacotes por execução (modelo) | Cabeçalho por pacote | Cabeçalhos estimados |
| --- | --- | --- | --- | --- |
| TCP | 5649 | 7 (SYN, SYN-ACK, ACK, FIN, ACK, FIN, ACK) | 52 B | 2008,0 KB |
| DNS | 1885 | 2 (consulta e resposta UDP) | 28 B | 103,1 KB |
| HTTPS | 942 | 14 (7 de controle TCP + ~7 segmentos de TLS e HTTP) | 52 B | 669,7 KB |

| | Por hora | Por dia |
| --- | --- | --- |
| Dados no socket (medido) | 737,2 KB | 17,28 MB |
| Cabeçalhos (modelo) | 531,3 KB | 12,45 MB |
| **Total estimado** | **1268,4 KB** | **29,73 MB** |

## Comparação com as metas do desenho

| Meta (docs/00-desenho-inicial.md) | Resultado |
| --- | --- |
| Tráfego ≤ 5 MB/h para o conjunto de sondas | 1268,4 KB/h → dentro da meta |
| Disco ~65 MB em 7 dias de dados brutos | 58,17 MB projetados em 7 dias medindo sem parar → dentro da meta |

## Detecção de suspensão comparada ao log de energia do macOS

O agente detecta suspensão pela diferença entre relógio civil e monotônico (limiar: um heartbeat, 5 s). Aqui cada lacuna `suspended` gravada no banco é comparada aos eventos `Sleep` → `Wake`/`DarkWake` de `pmset -g log` no mesmo período, com tolerância de 15 s nas duas pontas.

| Item | Resultado |
| --- | --- |
| Períodos de sono no log do macOS | 97 (o mais curto: 18s) |
| Detectados pelo agente | 97 |
| Não detectados | 0 |
| Lacunas do agente sem sono correspondente | 0 |
| Maior diferença no início / no fim | 10 s / 5 s |

## Limites desta medição

- Endpoints no próprio computador (loopback): mede o custo do agente, não a latência da internet. Os tamanhos de pacote TCP e TLS são os mesmos de uma rede real; o MTU de loopback é maior, mas as mensagens das sondas cabem num pacote.
- Cabeçalhos por modelo, não por captura de pacotes (que exigiria privilégio de administrador).
- CPU e memória vêm do `ps` uma vez por minuto; picos mais curtos que isso podem não aparecer.
- O computador ficou suspenso a maior parte do período. CPU e memória vêm só dos minutos acordados, e a maior parte dos despertares foi curta (manutenção do macOS), quando o agente recupera as sondas atrasadas.
- A comparação com o `pmset` valida a heurística só para o sono deste computador neste período. Sono mais curto que o limiar não é detectado, e relógio adiantado continua indistinguível de suspensão.
- A sonda UDP (F12) ainda não existe e não está incluída.
