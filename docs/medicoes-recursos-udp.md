# Medição de recursos do agente

Medição real no testbed local, gerada por `testbed/measure/measure.sh` e `testbed/measure/summarize` a partir dos arquivos brutos da pasta `2026-11-02-30m-udp` (`env.txt`, `samples.csv`, `counts.csv`, `coverage.csv`, `suspended.csv`, `pmset.csv`). Nada aqui é estimativa de desenvolvimento, exceto o que está marcado como modelo. Para regenerar: `go -C testbed/measure run ./summarize <pasta>`.

## Ambiente

| Item | Valor |
| --- | --- |
| inicio | 2026-11-03T02:24:39Z |
| fim | 2026-11-03T03:08:20Z |
| macos | 26.6.2 (25G83) |
| cpu | Apple M2, 8 núcleos |
| memoria_gb | 16 |
| go | go1.27.3 |
| agente | 0.7.0-dev |
| commit | 8446b02 |
| sondas | 3 endpoints × (tcp a cada 10 s, dns a cada 30 s, https a cada 60 s) + udp_echo a cada 15 s em 1 endpoint, intervalos padrão |
| tempo medindo, segundo o próprio agente | 0,46 h |
| computador suspenso, segundo o próprio agente | 0,26 h em 2 lacunas |
| amostras de 1 min do roteiro | 26 (0,43 h entre amostras seguidas; 0,28 h em intervalos > 2 min excluídos) |

As taxas por hora valem para o computador acordado e o agente medindo. Tempo suspenso não entra no denominador; a projeção de disco supõe operação contínua.

## Resultados

| Recurso | Medido |
| --- | --- |
| CPU, média (tempo de CPU total ÷ tempo medindo) | 0,100% de um núcleo |
| CPU, p95 dos minutos | 0,108% de um núcleo |
| CPU, pior minuto | 0,117% de um núcleo |
| Memória residente, média | 17,35 MB |
| Memória residente, máxima | 23,30 MB |
| Tráfego de dados no socket (nettop), entrada | 702,9 KB/h |
| Tráfego de dados no socket (nettop), saída | 623,9 KB/h |
| Execuções de sonda | 864 em 0,46 h medindo (1864/h; esperado com os intervalos padrão: 1860/h) |
| Banco de dados, crescimento por execução | 218 B |
| Banco de dados, crescimento por hora medindo | 396,9 KB |
| Banco de dados, tamanho final | 0,37 MB |

## Tráfego total com cabeçalhos

O `nettop` conta só os dados no socket. Os cabeçalhos IPv4 e TCP/UDP vêm de um modelo: pacotes por execução × tamanho do cabeçalho × execuções gravadas no banco.

| Sonda | Execuções | Pacotes por execução (modelo) | Cabeçalho por pacote | Cabeçalhos estimados |
| --- | --- | --- | --- | --- |
| TCP | 501 | 7 (SYN, SYN-ACK, ACK, FIN, ACK, FIN, ACK) | 52 B | 178,1 KB |
| DNS | 167 | 2 (consulta e resposta UDP) | 28 B | 9,1 KB |
| HTTPS | 85 | 14 (7 de controle TCP + ~7 segmentos de TLS e HTTP) | 52 B | 60,4 KB |
| UDP_ECHO | 111 | 40 (20 datagramas de ida e 20 ecos) | 28 B | 121,4 KB |

| | Por hora | Por dia |
| --- | --- | --- |
| Dados no socket (medido) | 1326,8 KB | 31,10 MB |
| Cabeçalhos (modelo) | 796,2 KB | 18,66 MB |
| **Total estimado** | **2123,0 KB** | **49,76 MB** |

## Comparação com as metas do desenho

| Meta (docs/00-desenho-inicial.md) | Resultado |
| --- | --- |
| Tráfego ≤ 5 MB/h para o conjunto de sondas | 2123,0 KB/h → dentro da meta |
| Disco ~65 MB em 7 dias de dados brutos | 65,12 MB projetados em 7 dias medindo sem parar → acima da meta |

## Detecção de suspensão comparada ao log de energia do macOS

O agente detecta suspensão pela diferença entre relógio civil e monotônico (limiar: um heartbeat, 5 s). Aqui cada lacuna `suspended` gravada no banco é comparada aos eventos `Sleep` → `Wake`/`DarkWake` de `pmset -g log` no mesmo período, com tolerância de 15 s nas duas pontas.

| Item | Resultado |
| --- | --- |
| Períodos de sono no log do macOS | 2 (o mais curto: 26s) |
| Detectados pelo agente | 2 |
| Não detectados | 0 |
| Lacunas do agente sem sono correspondente | 0 |
| Maior diferença no início / no fim | 3 s / 2 s |

## Limites desta medição

- Endpoints no próprio computador (loopback): mede o custo do agente, não a latência da internet. Os tamanhos de pacote TCP e TLS são os mesmos de uma rede real; o MTU de loopback é maior, mas as mensagens das sondas cabem num pacote.
- Cabeçalhos por modelo, não por captura de pacotes (que exigiria privilégio de administrador).
- CPU e memória vêm do `ps` uma vez por minuto; picos mais curtos que isso podem não aparecer.
- A comparação com o `pmset` valida a heurística só para o sono deste computador neste período. Sono mais curto que o limiar não é detectado, e relógio adiantado continua indistinguível de suspensão.
