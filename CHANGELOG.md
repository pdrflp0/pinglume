# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/); versões seguem [SemVer](https://semver.org/lang/pt-BR/). Enquanto a versão for `0.y.z`, API local, schema e interface podem mudar.

## [Não publicado]

### Adicionado

- Exportação de relatório (F8): HTML autocontido (sem script nem recurso externo, legível impresso, tema claro e escuro) e JSON com esquema `pinglume-export/1`, gerados do mesmo snapshot.

## [0.2.0] - 2026-10-25

Fatias F5, F6 e F7: sondas DNS e HTTPS com backoff, relatos "travou agora" com análise `incident-v1`, e comparação guiada `compare-v1` com detecção do tipo de rede. O Nível 1 ainda não está completo: faltam exportação (F8), retenção (F9), modo demo (F10), medição de recursos (F11) e sonda UDP (F12). Os endpoints próprios continuam não hospedados; o agente mede o `probe-server` local.

### Adicionado

**Sondas**

- Sonda DNS (`dns` v1): resolve um nome novo a cada execução (`<nonce>.<zona>`), para nenhum cache responder no lugar do servidor; NXDOMAIN, SERVFAIL e REFUSED viram `dns_error` (o código fica no detalhe) e ausência de resposta vira `timeout`. No testbed ela consulta o servidor de teste direto (`--dev-dns`), e o resolvedor usado entra nos parâmetros da definição.
- Sonda HTTPS (`https` v1): conexão nova a cada execução, por IP (não depende de DNS), sem proxy do sistema, sem seguir redirecionamentos, corpo limitado a 1 KB; certificado do testbed fixado com `--dev-ca` (a impressão digital entra nos parâmetros, então trocar o certificado cria outra definição). Grava as fases `connect`, `tls` e `ttfb`. Status próprios: `tls_error` (certificado, nome, alerta TLS) e `http_error` (status diferente de 200), separados de falhas de rede.
- Backoff por sonda: depois de 3 falhas seguidas o intervalo dobra a cada nova falha, até 8× o normal; volta ao normal na primeira resposta ok. Cancelamentos do próprio agente não contam como falha. Cada mudança é gravada (`probe_backoff`, `probe_backoff_end`), e o tempo em backoff aparece na CLI (coluna BACKOFF), no resumo do dashboard e como traço tracejado no topo de cada painel da linha do tempo.
- Flags do `pinglume run`: `--https-interval`, `--https-timeout`, `--dns-interval`, `--dns-timeout`, `--dev-ca`, `--dev-server-name`, `--dev-zone`, `--dev-dns` (repetível). Cada `--dev-target` ganha sonda HTTPS quando há `--dev-ca`.

**Relatos e análise `incident-v1`**

- Botão **Travou agora** no dashboard: diálogo nativo (`<dialog>`, foco preso, Esc fecha) com sintoma (áudio cortou, vídeo congelou, chamada caiu, lentidão, outro), "quando" (agora até 10 min atrás) e observação opcional de até 500 caracteres.
- CLI: `pinglume mark --symptom … --note … --ago …` e `pinglume incidents [--id …]`, com a análise completa no terminal.
- Relatos aparecem na linha do tempo como losango acima da cobertura e linha tracejada nos painéis, distintos das medições; clicar abre o relato. O painel de leitura do cursor também lista relatos no trecho.
- Cartão **Relatos** com veredito de cada relato e detalhe: o que as medições mostram (cada frase com as entradas que a sustentam), outras explicações possíveis, próximo teste e limites.
- Regra `incident-v1`: janela de 5 min antes a 1 min depois do relato; linha de base nas 2 h anteriores, mesma definição de sonda; degradação = falhas acima da base ou mediana acima de 2× a mediana e do p95 da base (com pelo menos 10 execuções ok na base). Vereditos: `aguardando_dados`, `sem_cobertura`, `degradacao_observada`, `sem_degradacao_detectavel`. A ressalva de que coincidência no tempo não prova causa é fixa; há teste garantindo que nenhum texto atribui culpa à operadora.
- Interpretação gravada e imutável quando a janela fecha (dados que chegam depois não a alteram). Exclusão de relato, com confirmação, apaga também a interpretação.

**Comparação guiada e `compare-v1`**

- Detecção do tipo de rede no macOS: interface da rota padrão (tabela de rotas do kernel) e tipo pela lista de portas do `networksetup` (Wi-Fi, cabo, VPN, outra). Só leitura; não coleta o nome da rede Wi-Fi. Gravada no início (`net_context`) e a cada mudança (`net_change`). Fora do macOS a interface fica "não detectada".
- Cartão **Comparação guiada** no dashboard: escolha da pergunta (cabo × Wi-Fi, perto do roteador, outra rede ou personalizada), duração de 10 a 30 min por janela, etapas com progresso, condição declarada e interface detectada em cada janela (com a origem visível), aviso quando a interface detectada antes da janela B é a mesma da A, cancelamento e histórico de comparações.
- Regra `compare-v1`: compara uma sonda só se ela tiver a mesma definição nas duas janelas, cada janela com pelo menos 10 min e cobertura de pelo menos 80%; senão, "não comparável" com o motivo. Diferença B − A da mediana de latência (com ≥ 10 execuções ok por janela) e da taxa de falha, com intervalo de 95% por bootstrap em blocos móveis (~√n observações, 1000 reamostragens, semente derivada do experimento: resultado reprodutível). "Melhora"/"piora" exigem intervalo excluindo zero e diferença além do limiar prático, max(10% da mediana de A, 1 ms) para latência e 2 p.p. para falhas; "sem diferença detectável" exige o intervalo inteiro dentro do limiar; melhora numa medida e piora na outra vira "inconclusivo". Aponta fatores de confusão: janelas separadas por mais de 2 h, interface que mudou durante a janela, VPN, interface igual nas duas janelas apesar da mudança declarada, e a ordem A→B, que mistura mudança e horário. Resultado gravado ao fim.

**API local**

- `GET/POST /api/v1/incidents`, `GET/DELETE /api/v1/incidents/{id}`.
- `GET /api/v1/network`, `GET/POST /api/v1/experiments`, `GET /api/v1/experiments/{id}`, `POST /api/v1/experiments/{id}/intervention`, `POST /api/v1/experiments/{id}/cancel`.
- Validação: sintoma conhecido, nota ≤ 500 caracteres, horário do relato nas últimas 24 h e não no futuro, janelas de 10 a 60 min, uma comparação ativa por vez (409), janela B só depois da A.
- O resumo e a linha do tempo trazem `backoffMs` por sonda e os períodos de backoff.

**probe-server (testbed)**

- HTTPS na mesma porta do TCP, com certificado autoassinado persistido em `testbed/tls/` (fora do git; chave com permissão 0600).
- DNS autoritativo mínimo (UDP) para a zona de teste: responde A com TTL 0, REFUSED fora da zona, ignora pacotes malformados.
- Limite simples de 20 requisições por segundo por IP.
- API de falhas só em loopback (`127.0.0.1:9900`, `PUT /faults`): `dns` (`ok`, `drop`, `servfail`, `nxdomain`), `https` (`ok`, `error500`, `hang`), `httpsDelay` e `listener` (`open`, `closed`).

**Banco de dados**

- Migração 003: tabelas `incidents` e `interpretations`.
- Migração 004: tabelas `experiments`, `experiment_windows`, `conditions` e `comparison_results`.
- Bancos da 0.1.0 são migrados automaticamente na primeira execução; o histórico existente é preservado.

**Desenvolvimento**

- CI roda também os testes do `probe-server`.
- `make test` roda os testes do `probe-server`; `make dev-server` e `make dev-agent` sobem o testbed completo (TCP, HTTPS e DNS).
- `docs/fluxo-git.md`: uma versão publicada por fatia.

### Alterado

- Rótulos de latência dizem o que cada tipo de sonda mede: handshake TCP, resolução de nome único, requisição HTTPS completa com conexão nova (CLI, dashboard e linha do tempo).
- Números nos textos das regras e do dashboard usam vírgula decimal.
- O botão de pausa no topo do dashboard passou a ser secundário; **Travou agora** é a ação principal.

### Corrigido

- Latências negativas (diferenças entre janelas) eram exibidas como "-25000 µs" no dashboard; agora "-25,0 ms".

Corrigido ainda durante o desenvolvimento desta versão, antes de publicar:

- A porta padrão do DNS de teste era 5353, que é a do mDNS (Bonjour) e já estava ocupada; o `probe-server` não subia. Agora é 15353.
- `incident-v1` tratava cobertura de forma simétrica: com menos de 50% da janela medida, um relato com falha evidente saía como "sem cobertura". Agora falha medida conta como evidência mesmo com cobertura parcial (com ressalva), e só a ausência de falha exige 50% de cobertura.
- Quando a linha de base não tinha execuções, o texto dizia "na linha de base, 0%"; agora diz que não há linha de base. Ressalvas repetidas por sonda foram agrupadas.
- `compare-v1` usava piso de 0,2 ms: no primeiro ensaio real, o DNS em loopback "piorou" 0,23 ms e anulou uma melhora de 26 ms no HTTPS. O piso passou a 1 ms (pacotes de áudio de chamada têm ~20 ms).
- O diálogo de relato prometia remover a observação da exportação, que ainda não existe; o texto foi corrigido.

### Removido

- Flags `-fault` e `-fault-after` do `probe-server`, substituídas pela API de falhas.

### Segurança

- As rotas novas seguem as mesmas proteções da API local: só `127.0.0.1`, token ou sessão, validação de `Host`, `Origin` e `Sec-Fetch-Site`, sem CORS. Criar, apagar e avançar etapas pelo navegador exige `Origin` igual ao do agente.
- A detecção de rede só lê o estado do sistema; nenhuma rota executa comando, muda configuração ou aceita alvo de sonda.

### Limites conhecidos

- Limiares de `incident-v1` e `compare-v1` são escolhas iniciais, ainda não calibradas com uso real.
- A comparação tem uma única rodada A→B; repetição A-B-A fica para o Nível 2.
- `incident-v1` ainda não usa o tipo de rede na linha de base.
- Em backoff, a volta de uma sonda demora mais para ser medida (até 8× o intervalo).
- Detecção de rede só no macOS; comparação sem comando de CLI.
- A escala de latência da linha do tempo é linear e compartilhada entre sondas.

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

[Não publicado]: https://github.com/pdrflp0/pinglume/compare/v0.2.0...develop
[0.2.0]: https://github.com/pdrflp0/pinglume/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/pdrflp0/pinglume/releases/tag/v0.1.0
