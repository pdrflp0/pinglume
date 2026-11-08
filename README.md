# Pinglume

Ferramenta local para investigar falhas intermitentes de internet durante chamadas: registra medições pequenas ao longo do tempo, marca quando houve medição e quando não houve, e guia comparações antes/depois.

**Estado:** Nível 1 completo (fatias F0–F12) e preparo de produção pronto (F13), ainda sem uso real. Os endpoints públicos ainda não estão no ar: o agente só mede o `probe-server` local (testbed) até a lista de produção ser preenchida ([roteiro de hospedagem](docs/hospedagem.md)). Desenho completo em [docs/00-desenho-inicial.md](docs/00-desenho-inicial.md).

## O que existe hoje

| Fatia | Situação |
| --- | --- |
| F0 repositório e toolchain | feito |
| F1 sonda TCP → SQLite → `pinglume status` | feito e testado |
| F2 cobertura: heartbeat, agente parado, suspensão, pausa | feito; suspensão conferida com o log de energia do macOS (97 de 97 sonos detectados) |
| F3 API local segura + dashboard servido pelo agente | feito e testado |
| F4 linha do tempo com lacunas, zoom, teclado e tabela equivalente | feito e testado |
| F5 sondas DNS e HTTPS, backoff e injeção de falhas no probe-server | feito e testado |
| F6 relatos ("travou agora") e análise `incident-v1` | feito e testado |
| F7 comparação guiada `compare-v1` e detecção do tipo de rede | feito e testado |
| F8 exportação HTML/JSON com prévia e redação | feito e testado |
| F9 retenção, compactação e limite de disco | feito e testado |
| F10 modo demonstração separado | feito e testado |
| F11 medição de recursos (CPU, memória, tráfego, disco) | feito: [24 h sem UDP](docs/medicoes-recursos.md), [30 min com UDP](docs/medicoes-recursos-udp.md) |
| F12 sonda UDP de perda e jitter | feito e testado |
| F13 endpoints de produção: lista embutida, certificado fixado, serviço e instalador | feito e ensaiado localmente; falta hospedar ([roteiro](docs/hospedagem.md)) |
| F14 comando `pinglume compare` (comparação guiada pela CLI) | feito e testado |
| F15 dashboard redesenhado: marca, tema escuro e claro, painel de abertura, grade de blocos e animações | feito e testado |
| Nível 2 em diante | ver backlog no documento de desenho |

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

### Comparação guiada

No dashboard, **Nova comparação** mede uma janela A (como está), pede a mudança (ex.: cabo no lugar do Wi-Fi) e mede uma janela B. Cada janela registra a condição declarada e a interface detectada (rota padrão + `networksetup`, só leitura).

O mesmo fluxo existe na CLI, com o agente rodando e o computador acordado durante as duas janelas:

```bash
pinglume compare new --question "o cabo melhora as quedas?" --change "trocar o Wi-Fi pelo cabo" --window 10m --condition "Wi-Fi, sala"
```

```bash
pinglume compare next --condition "cabo, sala"
```

`compare next` só funciona depois que a janela A terminou. `compare status` mostra a etapa, o tempo que falta e, ao fim, o resultado com as mesmas observações, explicações alternativas, próximo teste e limites do dashboard; `compare cancel` encerra uma comparação em andamento. Só uma comparação pode estar em andamento por vez.

A regra `compare-v1`:

- só compara uma sonda se ela tiver a mesma definição nas duas janelas, cada janela com pelo menos 10 min e cobertura de pelo menos 80%; senão, mostra "não comparável" e o motivo;
- calcula a diferença B − A da mediana de latência e da taxa de falha com intervalo de 95% por **bootstrap em blocos**, porque medições vizinhas no tempo não são independentes;
- só diz "melhora" ou "piora" quando o intervalo exclui zero **e** a diferença passa de um limiar prático (10% da mediana de A ou 1 ms, o que for maior — abaixo de 1 ms a diferença não muda uma chamada; 2 p.p. para falhas); "sem diferença detectável" exige o intervalo inteiro dentro do limiar; o resto é "inconclusivo";
- melhora numa medida e piora na outra vira "inconclusivo";
- aponta fatores de confusão: janelas distantes no tempo, interface que mudou durante a janela, VPN, e interface detectada igual nas duas janelas apesar da mudança declarada.

### Exportar um relatório

No dashboard, **Exportar relatório** mostra a prévia do arquivo e gera um HTML legível e um JSON (esquema `pinglume-export/1`) com o período exibido. Pela CLI:

```bash
agent/bin/pinglume export --data-dir .dev-data --window 24h --html relatorio.html --json relatorio.json
```

- Nunca saem: nome do computador, detalhes técnicos de erro, caminhos de arquivo, token.
- Substituídos em todos os textos: nome do dono embutido no nome do computador (padrão do macOS), e-mails, endereços IP (menos o loopback do testbed), endereços MAC, números longos e nomes de interface.
- Observações dos relatos só entram se você marcar (`--notes`), e passam pelas mesmas substituições.
- O relatório traz uma tabela do que foi removido, as versões das regras e o método; cada frase de análise vem com as entradas que a sustentam.
- Cada exportação gerada é um snapshot imutável com SHA-256 do JSON; o HTML cita esse hash. Um arquivo baixado não pode ser revogado.

### Modo demonstração

```bash
agent/bin/pinglume demo
```

```bash
agent/bin/pinglume open --demo
```

- Gera 3 h de histórico simulado com o roteiro completo (uso normal, agente desligado, degradação com relato "travou agora", comparação Wi-Fi × cabo, pausa, relato sem degradação) e continua ao vivo com uma falha curta a cada 12 minutos. Semente fixa: o resultado é igual a cada execução.
- Nada vai para a rede: as sondas são simuladas.
- Usa outro diretório (`Pinglume Demo`) e outra porta (47822), então roda ao lado do agente real. `--demo` funciona em `open`, `status`, `incidents`, `export` e `storage`.
- Nunca se mistura com dados reais: o banco guarda o modo; `run` recusa um banco de demonstração e `demo` recusa um banco com medições reais. A cada início a demo recria o próprio banco (só depois de confirmar que ele é de demonstração); `--keep` mantém o existente.
- Identificação: faixa "SIMULAÇÃO" fixa em todas as telas do dashboard (inclusive antes do login), título da aba com "[SIMULAÇÃO]", aviso na CLI `status`, e na exportação `"simulation": true` no JSON, faixa no HTML e arquivo `pinglume-demo-…`.

### Armazenamento

- Medições brutas ficam 7 dias (`--retention`); depois viram agregados por minuto (contagens exatas, mediana, p95, mínimo e máximo), guardados 90 dias (`--aggregate-retention`).
- Antes de compactar, as análises de relatos e comparações que dependem desses dados são calculadas e gravadas, para não mudarem depois.
- Limite de espaço do banco: 250 MB (`--disk-limit-mb`). Acima dele, o agente compacta mais cedo e, se preciso, descarta os agregados mais antigos; cada compactação e descarte vira um evento com período e motivo.
- Disco cheio de verdade: as medições são suspensas com o motivo "disco cheio" e voltam sozinhas quando houver espaço; o período fica registrado como sem medição.
- Períodos que incluem dados compactados mostram percentis marcados como aproximados ("aprox." no dashboard, "≈" na CLI).

```bash
agent/bin/pinglume storage --data-dir .dev-data
```

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

## Medição de recursos

```bash
make build
testbed/measure/measure.sh 1440 .measure/24h-$(date +%Y%m%d-%H%M)
go -C testbed/measure run ./summarize .measure/<pasta>
```

O roteiro sobe 3 probe-servers e um agente com os intervalos padrão, amostra CPU, memória (`ps`), dados no socket (`nettop`) e tamanho lógico do banco uma vez por minuto e, no fim, extrai do banco as contagens, a cobertura e as lacunas de suspensão, além dos eventos de sono do `pmset -g log`. Use `.measure/` (fora do git): o macOS apaga `/private/tmp` depois de alguns dias. Resultados publicados: [24 h sem a sonda UDP](docs/medicoes-recursos.md) e [rodada indicativa de 30 min com a sonda UDP](docs/medicoes-recursos-udp.md); dados brutos em [docs/medicoes/](docs/medicoes/).

## Hospedagem

[docs/hospedagem.md](docs/hospedagem.md) traz o passo a passo das contas, das VMs e da instalação com `deploy/deploy.sh`. Para ensaiar o formato da lista sem servidor público, gere uma entrada com `probe-server -print-endpoint` e passe um JSON com loopback em `pinglume run --dev-endpoints`.

## Desenvolvimento

Branches, versionamento (SemVer) e convenção de commits: [docs/fluxo-git.md](docs/fluxo-git.md). Histórico de versões: [CHANGELOG.md](CHANGELOG.md).

## Limites conhecidos

- No testbed, a sonda DNS consulta o servidor de teste diretamente (`--dev-dns`). Em produção ela usa o resolvedor do sistema, só para endpoints com `dns_zone`, e as duas são definições diferentes que nunca se comparam. Esse caminho de produção ainda não rodou contra um domínio real.
- A lista de produção é embutida no binário: trocar IP ou certificado de um endpoint exige recompilar o agente. A instalação foi ensaiada em contêiner Ubuntu 24.04, não numa VM dos provedores.
- Em backoff (até 8× o intervalo), a volta de uma sonda demora mais para aparecer: no ensaio, o DNS restaurado só foi medido ~22 s depois.
- A sonda UDP (`--dev-udp`) envia rajadas de 20 datagramas de 64 B a cada 20 ms (ritmo de áudio de chamada) e mede RTT, perda de datagramas e jitter. É perda de datagramas da sonda, não perda geral de pacotes IP. Por padrão roda a cada 15 s; é a sonda que mais gera tráfego, então é recomendada em um endpoint só.
- Os limiares da `incident-v1` (50% de cobertura, latência 2× a mediana e acima do p95 da base, 10 execuções ok de base) são escolhas iniciais, ainda não calibradas com uso real.
- A `incident-v1` ainda não usa o tipo de rede: a linha de base compara com as 2 h anteriores mesmo que a rede tenha mudado (os eventos `net_change` já são gravados).
- A comparação tem uma rodada A→B: mudança e horário se confundem, e o resultado sempre diz isso. Blocos repetidos (A-B-A) ficam para o Nível 2.
- A detecção de rede só existe no macOS; em outras plataformas a condição é apenas a declarada.
- A redação reconhece padrões (e-mail, IP, MAC, telefone, nome do computador). Nomes de outras pessoas escritos em texto livre não são detectáveis; por isso observações ficam fora por padrão.
- O p95 de períodos compactados é estimado a partir do p95 de cada minuto e tende a sair acima do real.
- `VACUUM` bloqueia gravações enquanto roda; ele só é usado quando há mais de 25% de espaço livre no arquivo ou o limite foi ultrapassado.
- A prévia em quadro (iframe) não pôde ser verificada no navegador embutido do app, que bloqueia quadros; o link "Abrir prévia em nova aba" foi verificado.
- Alvos de desenvolvimento precisam ser loopback ou rede privada; o agente recusa outros. Endereços públicos só entram pela lista embutida.
- A detecção de suspensão compara relógio civil e monotônico, com limiar de 5 s. Na medição de 24 h ela acertou os 97 períodos de sono do log de energia do macOS (diferença máxima de 10 s), mas isso vale para um Mac e um período; sono mais curto que o limiar não é detectado, e relógio adiantado manualmente continua indistinguível de sono (o motivo registrado diz isso).
- Depois de um crash, a lacuna começa no último heartbeat (a cada 5 s): o instante exato da queda tem até 5 s de incerteza.
- A linha do tempo usa escala linear compartilhada entre as sondas: um pico grande achata as demais. Escala logarítmica fica para depois do uso real.
- Os trechos (baldes) têm tamanho período ÷ 300; em 7 dias, cada trecho cobre ~34 min e a mediana esconde picos curtos dentro dele.
- Eventos do sistema (início/parada do agente) já vêm da API, mas ainda não aparecem como marcas no gráfico.
- Sessões do navegador ficam em memória: reiniciar o agente exige `pinglume open` de novo.
- Em largura de celular, a tabela de sondas rola horizontalmente dentro do cartão.
- A medição de recursos foi feita num único Mac (M2), com endpoints em loopback e o computador suspenso ~78% do período; CPU e memória vêm de ~4,4 h acordado. Cabeçalhos TCP/IP são estimados por modelo, não capturados.

## Segurança da API local

- Escuta só em `127.0.0.1:47821` (configurável com `--port`).
- Token de 256 bits em `api-token` (permissão 0600; o agente recusa arquivo legível por outros). A CLI usa o token; o navegador nunca o recebe.
- O navegador entra por `pinglume open`: código de uso único (60 s) → cookie `HttpOnly; SameSite=Strict`.
- `Host` diferente do próprio loopback → 421 (defesa contra DNS rebinding).
- Pedidos de outra origem (`Origin`, `Sec-Fetch-Site`) → 403, inclusive leituras. Mudança de estado por cookie exige `Origin` igual ao do agente.
- Nenhum cabeçalho CORS. CSP restritiva, `frame-ancestors 'none'`.
- Nenhuma rota executa comando, altera rede ou aceita alvo de sonda.

Os testes em `agent/internal/api/server_test.go` cobrem cada item.
