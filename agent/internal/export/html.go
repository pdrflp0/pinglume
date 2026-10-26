package export

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"
)

var stateLabels = map[string]string{
	"measuring": "Medindo", "paused": "Pausado", "suspended": "Computador suspenso", "agent_down": "Agente parado",
	"probe_unavailable": "Sonda indisponível", "shutting_down": "Encerrando", "unrecorded": "Sem registro",
}
var stateOrder = []string{"measuring", "paused", "suspended", "agent_down", "probe_unavailable", "unrecorded"}
var symptomLabels = map[string]string{
	"audio_cut": "Áudio cortou", "video_freeze": "Vídeo congelou", "dropped_call": "Chamada caiu", "slow": "Lentidão", "other": "Outro",
}
var verdictLabels = map[string]string{
	"aguardando_dados": "Aguardando dados", "sem_cobertura": "Sem cobertura", "degradacao_observada": "Degradação observada",
	"sem_degradacao_detectavel": "Sem degradação detectável", "melhora": "Melhora", "piora": "Piora",
	"sem_diferenca_detectavel": "Sem diferença detectável", "inconclusivo": "Inconclusivo", "nao_comparavel": "Não comparável",
	"dados_insuficientes": "Dados insuficientes",
}
var failureLabels = map[string]string{
	"timeout": "tempo esgotado", "refused": "conexão recusada", "dns_error": "erro de DNS", "tls_error": "erro de TLS",
	"http_error": "erro HTTP", "net_unreachable": "rede inalcançável", "cancelled": "cancelada pelo agente", "error": "outro erro",
}

func label(m map[string]string, k string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return k
}

func fmtLatency(us *int64) string {
	if us == nil {
		return "—"
	}
	v := float64(*us)
	if math.Abs(v) < 1000 {
		return fmt.Sprintf("%.0f µs", v)
	}
	return strings.Replace(fmt.Sprintf("%.2f ms", v/1000), ".", ",", 1)
}

func fmtDuration(ms int64) string {
	d := (time.Duration(ms) * time.Millisecond).Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	switch {
	case h > 0:
		return fmt.Sprintf("%d h %02d min", h, m)
	case m > 0:
		return fmt.Sprintf("%d min %02d s", m, s)
	}
	return fmt.Sprintf("%d s", s)
}

var funcs = template.FuncMap{
	"state":   func(k string) string { return label(stateLabels, k) },
	"symptom": func(k string) string { return label(symptomLabels, k) },
	"verdict": func(k string) string { return label(verdictLabels, k) },
	"failure": func(k string) string { return label(failureLabels, k) },
	"lat":     fmtLatency,
	"dur":     fmtDuration,
	"when": func(t time.Time) string {
		return t.Local().Format("02/01/2006 15:04:05")
	},
	"clock": func(t time.Time) string { return t.Local().Format("15:04:05") },
	// until mostra só o horário quando o fim é no mesmo dia do início, e a
	// data completa quando não é (senão uma lacuna de 25 h parece de 1 h).
	"until": func(start, end time.Time) string {
		a, b := start.Local(), end.Local()
		if a.YearDay() == b.YearDay() && a.Year() == b.Year() {
			return b.Format("15:04:05")
		}
		return b.Format("02/01/2006 15:04:05")
	},
	"pct": func(p *float64) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("%.0f%%", *p*100)
	},
	"upper": strings.ToUpper,
	"join":  strings.Join,
	"sourceLabel": func(s string) string {
		if s == "detected" {
			return "detectada"
		}
		return "declarada"
	},
	// bar devolve os segmentos da barra de cobertura com largura em %.
	"bar": func(c CoverageDoc) []struct {
		State string
		Pct   float64
	} {
		var total int64
		for _, v := range c.StateMS {
			total += v
		}
		var out []struct {
			State string
			Pct   float64
		}
		for _, k := range stateOrder {
			if v := c.StateMS[k]; v > 0 && total > 0 {
				out = append(out, struct {
					State string
					Pct   float64
				}{k, float64(v) * 100 / float64(total)})
			}
		}
		return out
	},
	"stateMS": func(c CoverageDoc) []struct {
		State string
		MS    int64
	} {
		var out []struct {
			State string
			MS    int64
		}
		for _, k := range stateOrder {
			if v := c.StateMS[k]; v > 0 {
				out = append(out, struct {
					State string
					MS    int64
				}{k, v})
			}
		}
		return out
	},
	"safeCSSWidth": func(p float64) template.CSS { return template.CSS(fmt.Sprintf("width:%.3f%%", p)) },
}

var page = template.Must(template.New("export").Funcs(funcs).Parse(`<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<title>Pinglume — relatório {{when .S.Period.From}} a {{when .S.Period.To}}</title>
<style>
:root{--bg:#fff;--text:#15181d;--muted:#5b6370;--border:#dde1e7;--soft:#f4f6f8;--measuring:#1f8a5b;--paused:#8a6d1f;--suspended:#6b5bb5;--agent_down:#b4432f;--probe_unavailable:#a1561b;--unrecorded:#9aa1ab}
@media (prefers-color-scheme:dark){:root{--bg:#14171c;--text:#e8ebef;--muted:#9aa3af;--border:#2b313a;--soft:#1c2027;--measuring:#3fbf86;--paused:#d1ad4f;--suspended:#9d8ef0;--agent_down:#f07a63;--probe_unavailable:#e59a5b;--unrecorded:#59616c}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:15px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,sans-serif}
main{max-width:920px;margin:0 auto;padding:24px 16px 48px}
h1{font-size:1.4rem;margin:0 0 4px}h2{font-size:1.1rem;margin:32px 0 8px;padding-top:8px;border-top:1px solid var(--border)}h3{font-size:.95rem;margin:16px 0 6px}
.muted{color:var(--muted)}.small{font-size:.85rem}.num{font-variant-numeric:tabular-nums;white-space:nowrap}
.notice{background:var(--soft);border:1px solid var(--border);border-radius:8px;padding:10px 14px;margin:16px 0}
.notice li{margin:2px 0}
.preview{border:2px dashed var(--agent_down);color:var(--agent_down);font-weight:700;padding:8px 12px;border-radius:8px;margin:12px 0}
.bar{display:flex;height:16px;border-radius:4px;overflow:hidden;background:var(--border);margin:8px 0}
.seg{height:100%}
.seg-measuring{background:var(--measuring)}
.seg-paused{background:repeating-linear-gradient(90deg,var(--paused) 0 4px,transparent 4px 7px)}
.seg-suspended{background:repeating-linear-gradient(45deg,var(--suspended) 0 3px,transparent 3px 7px)}
.seg-agent_down{background:repeating-linear-gradient(-45deg,var(--agent_down) 0 3px,transparent 3px 7px)}
.seg-probe_unavailable{background:repeating-linear-gradient(0deg,var(--probe_unavailable) 0 3px,transparent 3px 6px)}
.seg-unrecorded{background:repeating-linear-gradient(45deg,var(--unrecorded) 0 1px,transparent 1px 5px)}
table{width:100%;border-collapse:collapse;font-size:.9rem;margin:6px 0}
th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--border);vertical-align:top}
thead th{font-size:.78rem;text-transform:uppercase;letter-spacing:.03em;color:var(--muted)}
.r{text-align:right}
.tag{display:inline-block;font-size:.8rem;font-weight:600;padding:0 8px;border:1px solid currentColor;border-radius:999px}
.card{border:1px solid var(--border);border-radius:10px;padding:12px 16px;margin:12px 0}
ul{padding-left:20px}
.inputs{color:var(--muted);font-size:.82rem}
footer{margin-top:40px;font-size:.8rem;color:var(--muted);word-break:break-all}
@media print{.notice,.card{break-inside:avoid}body{font-size:12px}}
</style>
</head>
<body>
<main>
<h1>Relatório de conexão — Pinglume</h1>
<p class="muted">Período: <span class="num">{{when .S.Period.From}}</span> a <span class="num">{{when .S.Period.To}}</span> · gerado em <span class="num">{{when .S.GeneratedAt}}</span> · agente {{.S.AgentVersion}} · {{.S.Device.OS}}/{{.S.Device.Arch}}</p>
{{if .S.Preview}}<p class="preview">PRÉVIA — este conteúdo ainda não foi gerado como arquivo.</p>{{end}}
<div class="notice"><ul>{{range .S.Notice}}<li>{{.}}</li>{{end}}</ul></div>

<h2>Cobertura</h2>
<p class="small muted">{{.S.Method.Coverage}}</p>
<div class="bar" role="img" aria-label="Distribuição do período por estado de cobertura">{{range bar .S.Coverage}}<span class="seg seg-{{.State}}" style="{{safeCSSWidth .Pct}}"></span>{{end}}</div>
<table><thead><tr><th>Estado</th><th class="r">Tempo</th></tr></thead><tbody>
{{range stateMS .S.Coverage}}<tr><td>{{state .State}}</td><td class="r num">{{dur .MS}}</td></tr>{{end}}
</tbody></table>
{{if .S.Coverage.Gaps}}<h3>Períodos sem medição</h3>
<table><thead><tr><th>Início</th><th>Fim</th><th>Estado</th><th>Motivo</th></tr></thead><tbody>
{{range .S.Coverage.Gaps}}<tr><td class="num">{{when .Start}}</td><td class="num">{{until .Start .End}}</td><td>{{state .State}}</td><td class="muted">{{.Reason}}</td></tr>{{end}}
</tbody></table>{{end}}

<h2>Sondas</h2>
{{if .S.Probes}}<table><thead><tr><th>Sonda</th><th>O que mede</th><th class="r">Execuções</th><th class="r">Cobertura</th><th>Falhas</th><th class="r">P50</th><th class="r">P95</th></tr></thead><tbody>
{{range .S.Probes}}<tr><td><strong>{{upper .Kind}} v{{.ProtocolVersion}}</strong><div class="small muted">{{.Endpoint}} · {{.Target}}</div></td><td class="small">{{.Measures}}</td><td class="r num">{{.Total}}</td><td class="r num">{{pct .Coverage}}{{if .BackoffMS}}<div class="small muted">{{dur .BackoffMS}} em backoff</div>{{end}}</td><td class="small">{{if .Failures}}{{range $k, $v := .Failures}}{{$v}} {{failure $k}}<br>{{end}}{{else}}nenhuma{{end}}</td><td class="r num">{{lat .P50US}}</td><td class="r num">{{lat .P95US}}</td></tr>{{end}}
</tbody></table>
<p class="small muted">P50/P95 só de execuções bem-sucedidas. Falhas são rotuladas pelo protocolo da sonda; não são perda de pacotes IP.</p>
{{else}}<p class="muted">Nenhuma observação no período. Isso significa sem cobertura, não que a conexão esteve boa.</p>{{end}}

{{if .S.Included.Incidents}}<h2>Relatos</h2>
{{if not .S.Incidents}}<p class="muted">Nenhum relato no período.</p>{{end}}
{{range .S.Incidents}}<div class="card">
<p><strong>{{symptom .Symptom}}</strong> · <span class="num">{{when .ReportedAt}}</span> · <span class="tag">{{verdict .Analysis.Verdict}}</span></p>
{{if .Note}}<p class="small">Observação: “{{.Note}}”</p>{{end}}
<p class="small muted">Medições analisadas de {{clock .Analysis.WindowFrom}} a {{clock .Analysis.WindowTo}} (agente medindo {{dur .Analysis.MeasuringMS}} de {{dur .Analysis.WindowMS}}), comparadas com as 2 h anteriores · regra {{.Analysis.RuleVersion}}</p>
<h3>O que as medições mostram</h3><ul>{{range .Analysis.Findings}}<li>{{.Text}}<div class="inputs">Base: {{join .Inputs "; "}}</div></li>{{end}}</ul>
{{if .Analysis.Alternatives}}<h3>Outras explicações possíveis</h3><ul>{{range .Analysis.Alternatives}}<li>{{.}}</li>{{end}}</ul>{{end}}
{{if .Analysis.NextTest}}<h3>Próximo teste</h3><p>{{.Analysis.NextTest}}</p>{{end}}
{{if .Analysis.Caveats}}<h3>Limites</h3><ul class="small muted">{{range .Analysis.Caveats}}<li>{{.}}</li>{{end}}</ul>{{end}}
</div>{{end}}{{end}}

{{if .S.Included.Comparisons}}<h2>Comparações guiadas</h2>
{{if not .S.Comparisons}}<p class="muted">Nenhuma comparação concluída no período.</p>{{end}}
{{range .S.Comparisons}}<div class="card">
<p><strong>{{.Question}}</strong>{{if .Result}} · <span class="tag">{{verdict .Result.Verdict}}</span>{{end}}</p>
<p class="small muted">Mudança declarada: {{.Intervention}}</p>
<table><thead><tr><th>Janela</th><th>Período</th><th>Condições</th></tr></thead><tbody>
{{range .Windows}}<tr><td>{{if eq .Role "baseline"}}A (antes){{else}}B (depois){{end}}</td><td class="num">{{when .Start}}–{{until .Start .End}}</td><td class="small">{{range .Conditions}}{{.Key}}: {{.Value}} ({{sourceLabel .Source}})<br>{{else}}—{{end}}</td></tr>{{end}}
</tbody></table>
{{with .Result}}
<table><thead><tr><th>Sonda</th><th class="r">Mediana A</th><th class="r">Mediana B</th><th>Veredito</th></tr></thead><tbody>
{{range .Probes}}<tr><td>{{upper .Kind}} <span class="small muted">{{.Endpoint}}</span></td><td class="r num">{{lat .A.P50US}}</td><td class="r num">{{lat .B.P50US}}</td><td>{{verdict .Verdict}}{{if .Reasons}}<div class="small muted">{{join .Reasons "; "}}</div>{{end}}</td></tr>{{end}}
</tbody></table>
<h3>O que as medições mostram</h3><ul>{{range .Findings}}<li>{{.Text}}<div class="inputs">Base: {{join .Inputs "; "}}</div></li>{{end}}</ul>
<h3>Outras explicações possíveis</h3><ul>{{range .Alternatives}}<li>{{.}}</li>{{end}}</ul>
<h3>Próximo teste</h3><p>{{.NextTest}}</p>
{{if .Caveats}}<h3>Limites</h3><ul class="small muted">{{range .Caveats}}<li>{{.}}</li>{{end}}</ul>{{end}}
<p class="small muted">Regra {{.RuleVersion}}</p>
{{end}}
</div>{{end}}{{end}}

<h2>Método</h2>
<table><thead><tr><th>Sonda</th><th>O que mede</th></tr></thead><tbody>
{{range $k, $v := .S.Method.Probes}}<tr><td>{{upper $k}}</td><td class="small">{{$v}}</td></tr>{{end}}
</tbody></table>
<table><thead><tr><th>Regra</th><th>Resumo</th></tr></thead><tbody>
{{range $k, $v := .S.Method.Rules}}<tr><td class="num">{{$k}}</td><td class="small">{{$v}}</td></tr>{{end}}
</tbody></table>

<h2>O que foi removido deste relatório</h2>
<table><thead><tr><th>Dado</th><th>Tratamento</th><th class="r">Ocorrências</th></tr></thead><tbody>
{{range .S.Redactions}}<tr><td>{{.What}}</td><td class="small">{{.How}}</td><td class="r num">{{.Count}}</td></tr>{{end}}
</tbody></table>

<footer>
Esquema {{.S.SchemaVersion}} · identificador {{.S.ID}}{{if .Digest}} · SHA-256 do JSON correspondente: {{.Digest}}{{end}}
</footer>
</main>
</body>
</html>
`))

// RenderHTML gera o HTML autocontido. digest é o SHA-256 do JSON gerado junto
// (vazio na prévia).
func RenderHTML(s Snapshot, digest string) (string, error) {
	var b bytes.Buffer
	err := page.Execute(&b, struct {
		S      Snapshot
		Digest string
	}{S: s, Digest: digest})
	return b.String(), err
}
