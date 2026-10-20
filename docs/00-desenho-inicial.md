# Pinglume — desenho inicial (antes de programar)

Data: 20/10/2026 · Ambiente de referência: macOS 26.6.2, Apple Silicon (arm64)
Base: `01-pinglume-contexto-e-prompt.md`. Este documento é proposta de engenharia; nada aqui foi medido ainda.

---

## 0. Como ler este documento

Cada afirmação carrega uma etiqueta:

- **[REQ]** requisito do produto (vem da especificação ou foi proposto aqui e aceito).
- **[FATO]** algo verificável, com origem indicada. Os fatos sobre concorrentes vêm da pesquisa documental de 20/10/2026 na especificação e **não foram revalidados nesta sessão**; revalidar antes de qualquer decisão que dependa deles.
- **[HIP]** hipótese ainda não testada.
- **[EST]** estimativa de desenvolvimento. Não é resultado; vira número só depois de medida no hardware de referência.

---

## 1. Requisitos, fatos e hipóteses separados

### 1.1 Requisitos do núcleo local (Nível 1)

| ID | Requisito |
| --- | --- |
| R1 | [REQ] Um único executável inicia coleta, histórico e dashboard. Sem Docker, conta ou banco configurado pelo usuário. |
| R2 | [REQ] Sondas DNS, TCP e HTTPS contra três endpoints próprios predefinidos; cada métrica rotulada com protocolo e versão da sonda. Sonda `udp_echo` aceita como fatia F12. |
| R3 | [REQ] Ausência de medição é representada como "sem cobertura", nunca como "internet ok" ou "queda". |
| R4 | [REQ] Detectar e registrar: agente parado, computador suspenso, pausa do usuário, sonda indisponível. |
| R5 | [REQ] Marcação "travou agora" é registrada como **relato**, distinta de medição. |
| R6 | [REQ] Uma comparação guiada antes/depois (ex.: Wi-Fi → cabo) com regras de comparabilidade e resultado "inconclusivo" possível. |
| R7 | [REQ] Exportação HTML/JSON com prévia, redação de identificadores, período, lacunas, versão do coletor e método. |
| R8 | [REQ] Retenção configurável (padrão 7 dias de dados brutos) e limite de disco com comportamento definido. |
| R9 | [REQ] API somente em loopback, autenticada, com validação de Host/Origin e sem CORS amplo. |
| R10 | [REQ] Não captura conteúdo, não varre a rede local, não altera configurações do sistema. |
| R11 | [REQ] Modo demonstração separado, com falhas reproduzíveis e marcado visualmente como simulação. |

### 1.2 Fatos (da especificação, a revalidar)

- [FATO] Orb oferece monitoramento contínuo, histórico, recomendações, compartilhamento e API local também no plano gratuito; a análise local documentada usa Telegraf + InfluxDB + Grafana em containers e exige conta.
- [FATO] PingPlotter gratuito: um alvo, dez minutos de histórico (tabela oficial).
- [FATO] Cloudflare Speed Test mede velocidade, latência, jitter e perda pontualmente no navegador.
- [FATO, verificado nesta sessão] Ambiente local: macOS 26.6.2 arm64; Go 1.27.3 (Homebrew), Node v26.11.1, OpenJDK 21.0.12 e Docker presentes.

### 1.3 Hipóteses a testar

- [HIP] Pessoas com chamadas instáveis instalariam um agente local para investigar.
- [HIP] Os concorrentes não oferecem um fluxo de experimento com critérios de comparabilidade e resultado inconclusivo (não comprovado — apenas não encontrado na documentação).
- [HIP] Sondas TCP/HTTPS a cada poucos segundos capturam degradações que afetam chamadas. **Risco relevante**: chamadas usam UDP em tempo real; ver §5.4.
- [HIP] Um relatório com lacunas e limites explícitos é útil numa conversa real com o suporte.

---

## 2. Arquitetura

### 2.1 Visão geral

```
┌─────────────────────── máquina do usuário ───────────────────────┐
│  pinglume (binário Go único)                                     │
│   ├─ scheduler ──► probes (dns | tcp | https | udp-echo)         │
│   │     └─ budget/backoff/concurrency limiter                    │
│   ├─ coverage tracker (heartbeat, suspensão, pausa)              │
│   ├─ store (SQLite, WAL) ── migrations versionadas               │
│   ├─ analysis (regras determinísticas versionadas)               │
│   ├─ export (HTML/JSON + redação)                                │
│   └─ http (127.0.0.1:porta) ── API + SPA React embutida (embed)  │
└──────────────────────────────────────────────────────────────────┘
                 │ sondas pequenas, limitadas
                 ▼
   3 endpoints Pinglume (probe-server em Go, infraestrutura própria)
   — por ora: testbed local com falhas injetadas

Nível 2 (opt-in): agente ──fila durável──► Spring Boot ──► PostgreSQL
```

### 2.2 Fronteiras e justificativa

| Fronteira | Por quê | Alternativa descartada |
| --- | --- | --- |
| Agente em Go | Binário único sem runtime, timeouts e concorrência nativos (`context`, goroutines), bom acesso a rede. | Node/Electron: runtime pesado para um processo residente. Java: JVM residente consome memória e complica distribuição. |
| SQLite embutido, driver pure-Go (`modernc.org/sqlite`) | Sem CGO → compilação cruzada simples para Windows/Linux no Nível 2. | `mattn/go-sqlite3` (CGO) é mais rápido, mas complica cross-compile. Reavaliar se o desempenho medido exigir. |
| SPA React servida pelo próprio agente | Mesma origem → sem CORS; o usuário abre `http://127.0.0.1:<porta>`. A mesma SPA fala com API remota no Nível 2. | App desktop nativo: mais trabalho de plataforma sem ganho para o Nível 1. |
| probe-server próprio em Go | A especificação exige endpoints próprios e dimensionados; não medir contra serviços de terceiros em volume. | Medir contra Google/Cloudflare: viola o requisito e não controla mudanças do alvo. |
| Spring Boot + PostgreSQL só no Nível 2 | Contas, pareamento, ingestão e relatórios compartilhados exigem estado multiusuário. Monólito modular + worker de jobs. | Microserviços, Kafka, Kubernetes: sem volume que justifique. |

### 2.3 Versões

Política: versão estável mais recente no momento da instalação, fixada em `go.mod`, `package-lock.json` e (Nível 2) build Java, e registrada em `docs/versoes.md` com a data.

Observação: o Node instalado (v26) é linha "Current", não LTS. Para o build do dashboard é aceitável; para reprodutibilidade em CI, fixar uma versão LTS via `.nvmrc`.

### 2.4 Estrutura de repositório

```
pinglume/
  agent/            # módulo Go: cmd/pinglume, internal/{probe,sched,coverage,store,analysis,export,api,platform/darwin}
  probe-server/     # módulo Go: endpoint de sondas + injeção de falhas para testes
  web/              # React + TypeScript (Vite)
  cloud/            # Nível 2: Spring Boot (vazio até lá)
  testbed/          # docker-compose de desenvolvimento: probe-servers + falhas controladas
  docs/
```

---

## 3. Entidades (modelo inicial)

Todos os IDs são ULID (ordenáveis por tempo, gerados localmente, estáveis para sync futuro).

| Entidade | Campos principais | Notas |
| --- | --- | --- |
| **Device** | id, nome local, os, arch, agent_version, created_at | Um por instalação. Nome é redigível. |
| **Endpoint** | id, label, host, region, owner=`pinglume` | Só endpoints próprios no núcleo. |
| **ProbeDefinition** | id, kind (`dns`/`tcp`/`https`/`udp_echo`), endpoint_id, target, interval_ms, timeout_ms, **protocol_version**, params_json, active_from, active_to | Imutável: mudar parâmetro cria nova versão. Comparações exigem versões compatíveis. |
| **Observation** | id, device_id, probe_def_id, **seq** (por sonda), started_wall (UTC, ms), duration_us (monotônico), status (`ok`/`timeout`/`refused`/`dns_error`/`tls_error`/`http_error`/`net_unreachable`/`cancelled`), error_detail, tempos por fase (dns/connect/tls/ttfb), net_context_id, received_at | `duration` vem do relógio monotônico; `started_wall` só alinha com eventos e tem incerteza. |
| **NetContext** | id, interface_kind (`wifi`/`ethernet`/`vpn`/`other`/`unknown`), interface_name, gateway_hash, source (`detected`/`declared`), first_seen, last_seen | Mudança de contexto é fator de confusão explícito. SSID não é coletado por padrão (no macOS exige permissão de Localização). |
| **CoverageInterval** | id, device_id, start_wall, end_wall, state (`measuring`/`paused`/`suspended`/`agent_down`/`probe_unavailable`/`shutting_down`), reason, detected_by | Fonte da verdade para "havia medição?". |
| **Incident** | id, reported_at_wall, kind=`user_report`, symptom (`audio_cut`, `video_freeze`, `dropped_call`, `slow`, `other`), note, created_via | É um **relato**. Nunca derivado automaticamente no Nível 1. |
| **Condition** | id, experiment_window_id, key (`interface`, `location`, `time_of_day`…), value, source (`declared`/`detected`) | A origem aparece sempre na UI e na exportação. |
| **Experiment** | id, question, intervention, status, rule_version, created_at | Ex.: "Cabo melhora a latência em relação ao Wi-Fi no quarto?". |
| **ExperimentWindow** | id, experiment_id, role (`baseline`/`intervention`), start_wall, end_wall, probe_set_hash, coverage_ratio | Janela A e B. |
| **Interpretation** | id, experiment_id ou incident_id, rule_version, verdict (`melhora`/`piora`/`sem_diferença_detectável`/`inconclusivo`/`não_comparável`), inputs_json (IDs + estatísticas), alternatives_json, next_test, created_at | Cada frase aponta para entradas rastreáveis. |
| **ExportSnapshot** | id, created_at, period, selection_json, redaction_json, agent_version, method_version, sha256, format | Snapshot concluído é imutável. |
| **SystemEvent** | id, at_wall, kind (`agent_start`, `agent_stop`, `sleep_detected`, `wake`, `clock_jump`, `net_change`, `retention_compaction`, `disk_limit`), detail | Alimenta cobertura e a linha do tempo. |
| **SyncBatch** (Nível 2) | id, device_id, first_seq, last_seq, payload_hash, state, attempts | Idempotência por (device_id, probe_def_id, seq). |

---

## 4. Protocolo das sondas

### 4.1 Princípios

- Rótulo sempre inclui protocolo: "falha de conexão TCP para ep-2", nunca "perda de pacotes".
- Timeout de HTTP **não** é perda de pacotes IP; falha de DNS **não** é "internet caiu".
- Concorrência global limitada (padrão 2 sondas simultâneas), jitter aleatório no agendamento, backoff exponencial por endpoint após falhas consecutivas (o backoff fica registrado como redução de cobertura, não como falha).
- Testes pesados (ex.: vazão) não fazem parte do núcleo.

### 4.2 Sondas

| Sonda | O que mede | Como | Intervalo padrão [EST] |
| --- | --- | --- | --- |
| `dns` v1 | Tempo e sucesso de resolução pelo resolvedor do sistema | Resolve `<nonce>.p.<dominio-pinglume>` (curinga autoritativo próprio) para evitar cache; registra rcode. | 30 s |
| `tcp` v1 | Tempo de handshake TCP | `connect` em `ep:443` por IP já resolvido (isola do DNS), fecha imediatamente. | 10 s |
| `https` v1 | DNS+TCP+TLS+TTFB de uma requisição pequena | Conexão nova, `GET /p` → resposta ~64 B, sem cookies, `User-Agent: pinglume/<versão>`. Fases via `httptrace`. | 60 s |
| `udp_echo` v1 (F12) | Perda de datagramas UDP de aplicação, RTT e jitter | Rajada de 20 datagramas de 64 B a 20 ms (≈ ritmo de áudio VoIP), com seq e timestamp; servidor ecoa. Começa em 1 endpoint ou só durante sessões de investigação. | 1 rajada/15 s |

**Por que `udp_echo`:** chamadas trafegam em UDP com pacotes pequenos e frequentes; sondas a cada 10–60 s podem não ver rajadas de perda de poucos segundos. A métrica é rotulada "perda de datagramas UDP para ep-X", não perda IP geral.

### 4.3 probe-server

- Go, um binário: `GET /p` (HTTPS), eco UDP com limite por IP, DNS curinga (autoritativo próprio ou provedor com suporte a curinga).
- Rate limit por IP; logs de IP com retenção curta e documentada.
- Hospedagem: **adiada**. Desenvolvimento e testes de F1–F11 usam `testbed/` local com falhas injetadas.

### 4.4 Orçamento de tráfego [EST — metas, não resultados]

Por endpoint, por hora (incluindo cabeçalhos e handshake TLS 1.3):

| Sonda | Execuções/h | Bytes/execução [EST] | Total/h [EST] |
| --- | --- | --- | --- |
| dns | 120 | ~250 | ~30 KB |
| tcp | 360 | ~400 | ~144 KB |
| https (conexão nova) | 60 | ~6 KB | ~360 KB |
| udp_echo | 240 rajadas × 40 datagramas | ~92 B | ~880 KB |

Meta: **≤ 5 MB/h para o conjunto**. `udp_echo` domina o orçamento, por isso começa restrito. O real será medido com contadores por socket e conferido com `nettop`.

### 4.5 Orçamento de disco [EST]

- Observação bruta ≈ 120–160 B/linha com índices.
- Sem `udp_echo`: ~2.500 linhas/h → ~0,4 MB/h → **~65 MB em 7 dias**.
- `udp_echo` grava uma linha por rajada (enviados, recebidos, rtt p50/p95, jitter).
- Agregados por minuto guardados 90 dias: ~10 MB.
- Limite padrão: 250 MB, configurável.

**Ao atingir o limite:**
1. Compacta dados brutos mais antigos em agregados por minuto e registra `SystemEvent{retention_compaction}` com o período.
2. Se ainda acima, descarta agregados mais antigos e registra o período como lacuna de histórico.
3. Se o disco do sistema encher (erro de escrita): estado `probe_unavailable` com motivo `disk_full`, para de medir e avisa. Nunca perde dados em silêncio.

---

## 5. Modelo de cobertura

### 5.1 Estados

`measuring` · `paused` · `suspended` · `agent_down` (inferido) · `probe_unavailable` · `shutting_down`.

### 5.2 Detecção

- **Heartbeat** a cada 5 s grava `last_alive_wall` e `last_alive_mono`.
- **Agente parado**: no início, se `agora − last_alive_wall > 2× heartbeat`, cria `CoverageInterval{agent_down}` para a lacuna.
- **Suspensão**: diferença entre avanço do relógio civil e do monotônico entre heartbeats (no macOS o monotônico do Go não avança durante o sono). Fatia posterior pode assinar notificações de sono/despertar do IOKit; a heurística fica como fallback.
- **Relógio alterado**: salto civil sem salto monotônico e sem indício de sono → `SystemEvent{clock_jump}`; alinhamento daquele trecho ganha incerteza ampliada.
- **Mudança de rede**: polling leve da rota padrão e tipo de interface a cada 5 s → novo `NetContext` + evento.

### 5.3 Cobertura de uma janela

`coverage_ratio = execuções concluídas (exceto cancelled) / execuções esperadas no tempo em measuring`. A UI desenha lacunas hachuradas com legenda textual (não só cor).

### 5.4 Riscos de medição

| Risco | Efeito | Mitigação |
| --- | --- | --- |
| Granularidade vs. falhas de 1–3 s | Incidente real sem observação degradada | `udp_echo`; frequência maior temporária durante investigação, com orçamento visível |
| Cache DNS | Latência DNS irreal | Nonce em subdomínio curinga próprio |
| Problema do próprio endpoint | "Culpa da rede" falsa | 3 endpoints em infra distinta; falha só em 1 → hipótese "endpoint/rota" |
| Carga local | Latência inflada pelo próprio computador | Medir atraso de agendamento; avisar quando o agente atrasou |
| Timer coalescing / App Nap | Intervalos irregulares | Atraso de agendamento conta na cobertura |
| VPN, proxy | Rota diferente do uso real | Detectar `utun` e marcar no NetContext |
| Relógio civil impreciso | Desalinhamento relato × medição | Tolerância (±30 s) exibida |
| Marcação atrasada | Relato depois do evento | Janela assimétrica (−5 min / +1 min), configurável e exibida |

---

## 6. Regras de análise (versionadas)

### 6.1 Associação incidente ↔ medições (`incident-v1`)

Para um relato em `t`: janela `[t−5min, t+1min]`. Saída por sonda: cobertura, falhas, p50/p95, comparação com a linha de base das 2 h anteriores no mesmo NetContext. Resultados: "Sem cobertura suficiente", "Sem degradação detectável", "Degradação observada em [sonda/endpoint] — coincidência temporal, não prova de causa", sempre com alternativas (app/servidor da chamada, Wi-Fi local, endpoint) e próximo teste.

### 6.2 Comparação guiada (`compare-v1`)

**Pré-condições (senão `não_comparável`, com motivo):** mesmas ProbeDefinitions (versões e endpoints); cobertura ≥ 80 % por janela e sonda; duração mínima de 10 min por janela.
**Avisos** (entram nas alternativas): janelas a mais de 2 h, faixas horárias diferentes, mudança de NetContext não declarada, VPN.
**Resultado:** diferença de medianas e de taxa de falha por sonda, intervalo por bootstrap em blocos (respeita dependência temporal). `inconclusivo` quando o intervalo cruza zero ou a amostra é pequena. Nunca "culpa da operadora".

---

## 7. Segurança da API local

- Escuta só em `127.0.0.1`/`::1`, porta configurável.
- Token de 256 bits em arquivo `0600` no diretório de dados.
- Dashboard: `pinglume open` gera código de uso único (TTL 60 s) → `GET /auth?code=…` troca por cookie `HttpOnly; SameSite=Strict`. CLI usa o token.
- `Host` só `127.0.0.1:<porta>` / `localhost:<porta>` (contra DNS rebinding).
- Requisições que alteram estado exigem `Origin` igual à própria origem; sem cabeçalhos CORS.
- Nenhum endpoint executa comando, altera rede ou aceita alvo arbitrário de sonda.

---

## 8. Exportação e redação

- Prévia obrigatória.
- Redação padrão: nome do dispositivo, interface, hash do gateway, IPs locais, notas livres (inclusão opcional), caminhos. IPs dos endpoints Pinglume permanecem.
- Conteúdo: período, lacunas, versões (agente, sondas, regras), método, cada frase com referência a IDs de observação/regra.
- HTML autocontido + JSON com schema versionado.
- Aviso: arquivo baixado não é revogável.
- Teste: dataset com identificadores sensíveis conhecidos → export → busca garante ausência.

---

## 9. Matriz competitiva → experimentos verificáveis

| Requisito diferencial | Experimento | Critério de aprovação |
| --- | --- | --- |
| Instalação única, sem Docker/conta | Sessão observada: instalar → marcar incidente → histórico → exportar; contar passos, erros e ajuda. Repetir com a rota local documentada do Orb. | Conclusão sem ajuda; passos documentados lado a lado. Sem alegar superioridade antes dos dados. |
| 7 dias locais, 3 endpoints | Passagem de tempo simulada (relógio injetado) + ensaio real de 7 dias | Retenção correta; disco e tráfego medidos publicados com ambiente |
| Comparação justa | Conjuntos equivalentes e propositalmente incompatíveis | 100 % dos incompatíveis marcados `não_comparável` com motivo correto |
| Conclusões com alternativas | Injetar: agente desligado, endpoint bloqueado, troca de interface, DNS falha com HTTPS ok | Nenhum veredito culpa a operadora; lacunas viram "sem cobertura" |
| Exportação segura | Cenários com dados sensíveis conhecidos | Zero ocorrências após redação; leitor externo rastreia cada frase |

Revisitar o recorte se um concorrente já fizer a comparação guiada tão bem quanto a proposta.

---

## 10. Backlog por fatias verticais (Nível 1)

| # | Fatia | Critérios de aceitação |
| --- | --- | --- |
| F0 | Repositório, toolchain, `make test` | Go e web testam; versões em `docs/versoes.md`; git inicializado |
| F1 | Agente mínimo: sonda TCP → SQLite → `pinglume status` | Roda contra probe-server local; observações com seq, monotônico e status; migração 001; Ctrl+C encerra em ≤ 2 s sem linha corrompida |
| F2 | Cobertura: heartbeat, agente parado, suspensão, pausa | Testes com relógio injetado (sono, salto, crash); `pause/resume` para novas medições em ≤ 1 s |
| F3 | API local segura + SPA servida pelo agente | Host inválido → recusa; Origin estranho em POST → 403; sem cookie → 401; nenhum cabeçalho CORS |
| F4 | Linha do tempo com lacunas | Lacunas hachuradas e rotuladas, zoom, tabela acessível equivalente, teclado, `prefers-reduced-motion` |
| F5 | DNS + HTTPS + injeção de falhas no probe-server | "DNS falha, HTTPS por IP responde" rotulado corretamente; backoff aparece como redução de cobertura |
| F6 | Marcar incidente + `incident-v1` | Relato separado visualmente de medição; cobertura, alternativas e próximo teste |
| F7 | Comparação guiada `compare-v1` | Casos de §9 linha 3 passam |
| F8 | Exportação com redação | Teste de ausência de identificadores; snapshot imutável com sha256 |
| F9 | Retenção e limite de disco | Teste de disco cheio, compactação registrada, sem perda silenciosa |
| F10 | Modo demo | Banco separado, faixa "SIMULAÇÃO" em telas e exportação |
| F11 | Medição de recursos | CPU, memória, disco e bytes/h medidos 24 h no Mac de referência e publicados |
| F12 | `udp_echo` | Perda injetada é detectada; rótulo correto |

Nível 2 será detalhado depois que o Nível 1 rodar com uso real.

---

## 11. Plano de descoberta de usuários (do zero)

Nada abaixo é executado sem instrução explícita.

1. Ler discussões existentes sobre chamadas travando em comunidades de trabalho remoto, TI e redes, respeitando regras de autopromoção. Registrar sintoma, o que tentaram e ferramenta usada.
2. Com o Nível 1 rodando: 5–8 sessões observadas com pessoas com problema recente. Medir instalação, incidente com cobertura, comparação concluída, compreensão do resultado, ação tomada e por que preferiria Orb/PingPlotter/suporte.
3. Sinais: volta com outro incidente; usa o relatório com o suporte. Revisão: ninguém instala, o resultado não muda decisão, ou ferramenta existente resolve com menos esforço.

---

## 12. Decisões

| Decisão | Situação |
| --- | --- |
| Instalar Go | Feito: Go 1.27.3 via Homebrew |
| Local do repositório | `~/dev/pinglume` |
| Sonda `udp_echo` | Aceita, fatia F12, começa restrita |
| Hospedagem dos endpoints e domínio | Adiada; só `testbed/` local até a medição real |
| Licença do repositório | Em aberto |
