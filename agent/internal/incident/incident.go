// Package incident associa um relato ("travou agora") às medições próximas
// e produz uma interpretação descritiva, com limites explícitos.
//
// Regra incident-v1:
//   - janela de análise: [relato − 5 min, relato + 1 min] (a pessoa costuma
//     marcar depois do problema);
//   - linha de base: as 2 h anteriores à janela, mesma definição de sonda;
//   - falha ou lentidão medida na janela é evidência mesmo com cobertura
//     parcial (o resultado traz a ressalva); já a ausência de degradação só
//     conta se o agente mediu ao menos 50% da janela — senão, "sem cobertura";
//   - degradação numa sonda: alguma falha na janela com taxa acima da linha de
//     base, ou mediana da janela acima de 2× a mediana e acima do p95 da base
//     (exige ≥ 10 execuções ok na base);
//   - coincidência temporal nunca é apresentada como causa.
package incident

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"pinglume/agent/internal/store"
)

const (
	RuleVersion    = "incident-v1"
	Before         = 5 * time.Minute
	After          = time.Minute
	BaselineSpan   = 2 * time.Hour
	minCoverage    = 0.5
	minBaselineOKs = 10
)

const (
	VerdictPending     = "aguardando_dados"
	VerdictNoCoverage  = "sem_cobertura"
	VerdictDegradation = "degradacao_observada"
	VerdictNoChange    = "sem_degradacao_detectavel"
)

type Stats struct {
	N        int            `json:"n"`
	OK       int            `json:"ok"`
	Failures map[string]int `json:"failures"`
	FailRate float64        `json:"failRate"`
	P50US    *int64         `json:"p50Us"`
	P95US    *int64         `json:"p95Us"`
}

type ProbeResult struct {
	ProbeDefID      string `json:"probeDefId"`
	Endpoint        string `json:"endpoint"`
	Kind            string `json:"kind"`
	ProtocolVersion int    `json:"protocolVersion"`
	Window          Stats  `json:"window"`
	Baseline        Stats  `json:"baseline"`
	// Coverage: execuções na janela ÷ esperadas pelo tempo medindo.
	Coverage *float64 `json:"coverage"`
	Failing  bool     `json:"failing"`
	Slow     bool     `json:"slow"`
	// NoBaseline: base insuficiente para julgar a latência.
	NoBaseline bool `json:"noBaseline"`
}

// Finding é uma frase da interpretação e as entradas que a sustentam.
type Finding struct {
	Text       string   `json:"text"`
	ProbeDefID string   `json:"probeDefId,omitempty"`
	Inputs     []string `json:"inputs"`
}

type Analysis struct {
	RuleVersion  string        `json:"ruleVersion"`
	Verdict      string        `json:"verdict"`
	WindowFrom   time.Time     `json:"windowFrom"`
	WindowTo     time.Time     `json:"windowTo"`
	BaselineFrom time.Time     `json:"baselineFrom"`
	MeasuringMS  int64         `json:"measuringMs"`
	WindowMS     int64         `json:"windowMs"`
	Probes       []ProbeResult `json:"probes"`
	Findings     []Finding     `json:"findings"`
	Alternatives []string      `json:"alternatives"`
	NextTest     string        `json:"nextTest"`
	Caveats      []string      `json:"caveats"`
	// Final: a janela já fechou; a análise não muda mais e foi gravada.
	Final bool `json:"final"`
}

// Analyze calcula a interpretação. liveUntil estende o intervalo de
// cobertura aberto (só o agente vivo passa "agora").
func Analyze(ctx context.Context, st *store.Store, inc store.Incident, now, liveUntil time.Time) (Analysis, error) {
	t := inc.ReportedAt
	a := Analysis{
		RuleVersion:  RuleVersion,
		WindowFrom:   t.Add(-Before),
		WindowTo:     t.Add(After),
		BaselineFrom: t.Add(-Before - BaselineSpan),
		Probes:       []ProbeResult{},
		Findings:     []Finding{},
		Caveats:      []string{},
	}
	a.WindowMS = a.WindowTo.Sub(a.WindowFrom).Milliseconds()
	a.Final = !now.Before(a.WindowTo)
	winTo := a.WindowTo
	if !a.Final {
		winTo = now
	}

	ivs, err := st.Intervals(ctx, a.WindowFrom, winTo)
	if err != nil {
		return a, err
	}
	var measuring time.Duration
	for _, iv := range store.ExtendLive(ivs, liveUntil, winTo) {
		if iv.State == "measuring" {
			measuring += iv.Duration()
		}
	}
	a.MeasuringMS = measuring.Milliseconds()

	rows, err := st.ObservationsBetween(ctx, a.BaselineFrom, winTo)
	if err != nil {
		return a, err
	}
	type acc struct {
		res          ProbeResult
		interval     time.Duration
		winOK, baseO []time.Duration
	}
	byID := map[string]*acc{}
	var order []string
	for _, r := range rows {
		x := byID[r.ProbeDefID]
		if x == nil {
			x = &acc{res: ProbeResult{ProbeDefID: r.ProbeDefID, Endpoint: r.EndpointLabel, Kind: r.Kind,
				ProtocolVersion: r.ProtocolVersion,
				Window:          Stats{Failures: map[string]int{}}, Baseline: Stats{Failures: map[string]int{}}},
				interval: r.Interval}
			byID[r.ProbeDefID] = x
			order = append(order, r.ProbeDefID)
		}
		if r.Status == "cancelled" {
			continue // cancelamento é do agente, não diz nada da rede
		}
		s, oks := &x.res.Baseline, &x.baseO
		if !r.Started.Before(a.WindowFrom) {
			s, oks = &x.res.Window, &x.winOK
		}
		s.N++
		if r.Status == "ok" {
			s.OK++
			*oks = append(*oks, r.Duration)
		} else {
			s.Failures[r.Status]++
		}
	}
	for _, id := range order {
		x := byID[id]
		finish(&x.res.Window, x.winOK)
		finish(&x.res.Baseline, x.baseO)
		if exp := float64(measuring) / float64(x.interval); exp >= 1 {
			c := min(1, float64(x.res.Window.N)/exp)
			x.res.Coverage = &c
		}
		w, b := x.res.Window, x.res.Baseline
		x.res.Failing = w.N > 0 && w.N-w.OK > 0 && w.FailRate > b.FailRate
		x.res.NoBaseline = b.OK < minBaselineOKs
		if !x.res.NoBaseline && w.P50US != nil && b.P50US != nil && b.P95US != nil {
			x.res.Slow = *w.P50US > 2**b.P50US && *w.P50US > *b.P95US
		}
		a.Probes = append(a.Probes, x.res)
	}
	sort.SliceStable(a.Probes, func(i, j int) bool {
		if a.Probes[i].Endpoint != a.Probes[j].Endpoint {
			return a.Probes[i].Endpoint < a.Probes[j].Endpoint
		}
		return a.Probes[i].Kind < a.Probes[j].Kind
	})

	interpret(&a)
	return a, nil
}

func finish(s *Stats, oks []time.Duration) {
	if s.N > 0 {
		s.FailRate = float64(s.N-s.OK) / float64(s.N)
	}
	if len(oks) == 0 {
		return
	}
	sort.Slice(oks, func(i, j int) bool { return oks[i] < oks[j] })
	p50 := oks[int(float64(len(oks)-1)*0.5+0.5)].Microseconds()
	p95 := oks[int(float64(len(oks)-1)*0.95+0.5)].Microseconds()
	s.P50US, s.P95US = &p50, &p95
}

var kindLabel = map[string]string{"tcp": "TCP", "dns": "DNS", "https": "HTTPS"}

func probeName(p ProbeResult) string {
	k := kindLabel[p.Kind]
	if k == "" {
		k = strings.ToUpper(p.Kind)
	}
	return fmt.Sprintf("%s (%s)", k, p.Endpoint)
}

func ms(us int64) string {
	if us < 1000 {
		return fmt.Sprintf("%d µs", us)
	}
	return fmt.Sprintf("%.1f ms", float64(us)/1000)
}

var failureLabels = map[string]string{
	"timeout": "tempo esgotado", "refused": "conexão recusada", "dns_error": "erro de DNS",
	"tls_error": "erro de TLS", "http_error": "erro HTTP", "net_unreachable": "rede inalcançável", "error": "outro erro",
}

func failureLabel(s string) string {
	if l, ok := failureLabels[s]; ok {
		return l
	}
	return s
}

func pct(r float64) string { return fmt.Sprintf("%.0f%%", r*100) }

func interpret(a *Analysis) {
	measuredFrac := float64(a.MeasuringMS) / float64(a.WindowMS)
	executed := 0
	for _, p := range a.Probes {
		executed += p.Window.N
	}
	coverageInput := fmt.Sprintf("medindo %s de %s da janela",
		(time.Duration(a.MeasuringMS) * time.Millisecond).Round(time.Second), time.Duration(a.WindowMS)*time.Millisecond)

	if !a.Final {
		a.Verdict = VerdictPending
		a.Caveats = append(a.Caveats, fmt.Sprintf("A janela de análise só fecha às %s; o resultado ainda pode mudar.",
			a.WindowTo.Local().Format("15:04:05")))
	}

	noCoverage := func() {
		if a.Final {
			a.Verdict = VerdictNoCoverage
		}
		a.Findings = append(a.Findings, Finding{
			Text: fmt.Sprintf("O agente mediu só %s da janela em torno do relato e não registrou degradação no que mediu. "+
				"Isso não basta para dizer se a rede estava boa ou ruim nesse momento.", pct(measuredFrac)),
			Inputs: []string{coverageInput, fmt.Sprintf("%d execuções na janela", executed)},
		})
		a.Alternatives = []string{"O agente estava parado, pausado ou o computador suspenso durante parte do episódio."}
		a.NextTest = "Deixe o agente rodando durante as próximas chamadas e marque o próximo episódio assim que ele acontecer."
	}
	if executed == 0 {
		noCoverage()
		return
	}

	var failing, slow []ProbeResult
	kinds := map[string]bool{}
	for _, p := range a.Probes {
		if p.Window.N == 0 {
			continue
		}
		if p.Failing {
			failing = append(failing, p)
			kinds[p.Kind] = true
		}
		if p.Slow {
			slow = append(slow, p)
			kinds[p.Kind] = true
		}
	}
	for _, p := range failing {
		fails := []string{}
		for k, v := range p.Window.Failures {
			fails = append(fails, fmt.Sprintf("%d %s", v, failureLabel(k)))
		}
		sort.Strings(fails)
		base := fmt.Sprintf("na linha de base, %s de %d execuções falharam", pct(p.Baseline.FailRate), p.Baseline.N)
		if p.Baseline.N == 0 {
			base = "não há linha de base (nenhuma execução nas 2 h anteriores) para saber se isso é incomum"
		}
		a.Findings = append(a.Findings, Finding{
			Text: fmt.Sprintf("%s falhou em %d de %d execuções na janela (%s); %s.",
				probeName(p), p.Window.N-p.Window.OK, p.Window.N, strings.Join(fails, ", "), base),
			ProbeDefID: p.ProbeDefID,
			Inputs: []string{fmt.Sprintf("janela: %d execuções", p.Window.N),
				fmt.Sprintf("base: %d execuções, %s de falha", p.Baseline.N, pct(p.Baseline.FailRate))},
		})
	}
	for _, p := range slow {
		a.Findings = append(a.Findings, Finding{
			Text: fmt.Sprintf("%s ficou mais lenta: mediana de %s na janela contra %s na linha de base (p95 da base: %s).",
				probeName(p), ms(*p.Window.P50US), ms(*p.Baseline.P50US), ms(*p.Baseline.P95US)),
			ProbeDefID: p.ProbeDefID,
			Inputs:     []string{fmt.Sprintf("janela: %d ok", p.Window.OK), fmt.Sprintf("base: %d ok", p.Baseline.OK)},
		})
	}

	if measuredFrac < 0.9 {
		a.Caveats = append(a.Caveats, fmt.Sprintf("Cobertura parcial: o agente mediu %s da janela.", pct(measuredFrac)))
	}
	var thin []string
	for _, p := range a.Probes {
		if p.NoBaseline && p.Window.N > 0 {
			thin = append(thin, fmt.Sprintf("%s (%d ok)", probeName(p), p.Baseline.OK))
		}
	}
	if len(thin) > 0 {
		a.Caveats = append(a.Caveats, fmt.Sprintf("Linha de base curta demais para julgar latência (mínimo %d execuções ok nas 2 h anteriores): %s.",
			minBaselineOKs, strings.Join(thin, ", ")))
	}
	a.Caveats = append(a.Caveats, "As sondas rodam a cada poucos segundos; uma interrupção mais curta que esse intervalo pode não aparecer.")

	if len(failing)+len(slow) == 0 {
		if measuredFrac < minCoverage {
			a.Caveats = nil // as ressalvas acima pressupõem medições suficientes
			noCoverage()
			return
		}
		if a.Final {
			a.Verdict = VerdictNoChange
		}
		a.Findings = append(a.Findings, Finding{
			Text:   "Nenhuma sonda mostrou falha ou lentidão acima da linha de base na janela do relato.",
			Inputs: []string{coverageInput, fmt.Sprintf("%d execuções na janela", executed)},
		})
		a.Alternatives = []string{
			"O problema pode estar no serviço da chamada (servidores do aplicativo), não no caminho medido.",
			"Uma queda mais curta que o intervalo das sondas pode ter passado entre duas medições.",
			"Carga no próprio computador (CPU, Wi-Fi ocupado por outro programa) afeta a chamada sem afetar sondas pequenas.",
		}
		a.NextTest = "Marque os próximos episódios. Se vários relatos seguidos não coincidirem com degradação, o problema provavelmente não está entre este computador e os endpoints medidos."
		return
	}

	if a.Final {
		a.Verdict = VerdictDegradation
	}
	a.Findings = append(a.Findings, Finding{
		Text:   "A degradação coincide com o relato no tempo. Isso não prova que ela causou o problema da chamada.",
		Inputs: []string{"regra " + RuleVersion},
	})
	switch {
	case len(kinds) == 1 && kinds["dns"]:
		a.Alternatives = []string{
			"Problema no servidor DNS usado, com o restante do caminho funcionando (as outras sondas não degradaram).",
			"Uma chamada já conectada costuma não depender de DNS; o episódio pode ter outra causa.",
		}
	default:
		a.Alternatives = []string{
			"Problema no trecho entre este computador e a internet: Wi-Fi, roteador ou provedor. As sondas não conseguem separar esses trechos sozinhas.",
			"Problema nos endpoints medidos ou na rota até eles (no testbed local, os endpoints estão neste computador).",
			"Outro dispositivo ou programa ocupando a conexão nesse momento.",
		}
	}
	a.NextTest = "Faça uma comparação guiada: repita uma janela de 10 minutos por cabo (ou perto do roteador) e compare com o Wi-Fi atual."
}

// Load devolve a interpretação gravada para o relato ou calcula uma nova.
// Quando a janela já fechou, o resultado é gravado e passa a ser imutável
// para esta versão da regra.
func Load(ctx context.Context, st *store.Store, inc store.Incident, now, liveUntil time.Time) (Analysis, error) {
	if body, ok, err := st.Interpretation(ctx, inc.ID, RuleVersion); err != nil {
		return Analysis{}, err
	} else if ok {
		var a Analysis
		if err := json.Unmarshal([]byte(body), &a); err == nil {
			return a, nil
		}
	}
	a, err := Analyze(ctx, st, inc, now, liveUntil)
	if err != nil || !a.Final {
		return a, err
	}
	body, err := json.Marshal(a)
	if err != nil {
		return a, err
	}
	return a, st.SaveInterpretation(ctx, inc.ID, RuleVersion, a.Verdict, string(body), now)
}
