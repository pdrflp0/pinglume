package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/store"
)

const (
	RuleVersion     = "compare-v1"
	MinWindow       = 10 * time.Minute
	minCoverage     = 0.8
	minOKForLatency = 10
	maxGap          = 2 * time.Hour
	bootstrapRounds = 1000

	latencyFloorUS   = 1000
	latencyRelative  = 0.10
	failureThreshold = 0.02
)

const (
	PhaseBaseline     = "baseline_running"
	PhaseAwaiting     = "awaiting_intervention"
	PhaseIntervention = "intervention_running"
	PhaseDone         = "done"
	PhaseCancelled    = "cancelled"
)

const (
	Better        = "melhora"
	Worse         = "piora"
	NoDifference  = "sem_diferenca_detectavel"
	Inconclusive  = "inconclusivo"
	NotComparable = "nao_comparavel"
	Insufficient  = "dados_insuficientes"
)

func windowEnd(w store.Window) time.Time {
	if w.End != nil {
		return *w.End
	}
	return w.PlannedEnd
}

func windowByRole(e store.Experiment, role string) *store.Window {
	for i := range e.Windows {
		if e.Windows[i].Role == role {
			return &e.Windows[i]
		}
	}
	return nil
}

func Phase(e store.Experiment, now time.Time) string {
	if e.CancelledAt != nil {
		return PhaseCancelled
	}
	a, b := windowByRole(e, "baseline"), windowByRole(e, "intervention")
	switch {
	case a == nil || now.Before(windowEnd(*a)):
		return PhaseBaseline
	case b == nil:
		return PhaseAwaiting
	case now.Before(windowEnd(*b)):
		return PhaseIntervention
	}
	return PhaseDone
}

type WindowStats struct {
	N        int     `json:"n"`
	OK       int     `json:"ok"`
	FailRate float64 `json:"failRate"`
	P50US    *int64  `json:"p50Us"`
	Coverage float64 `json:"coverage"`
}

type Effect struct {
	Estimate  float64 `json:"estimate"`
	CILow     float64 `json:"ciLow"`
	CIHigh    float64 `json:"ciHigh"`
	Threshold float64 `json:"threshold"`
	Verdict   string  `json:"verdict"`
}

type ProbeComparison struct {
	ProbeDefID      string      `json:"probeDefId"`
	Endpoint        string      `json:"endpoint"`
	Kind            string      `json:"kind"`
	ProtocolVersion int         `json:"protocolVersion"`
	Comparable      bool        `json:"comparable"`
	Reasons         []string    `json:"reasons"`
	A               WindowStats `json:"a"`
	B               WindowStats `json:"b"`
	Latency         *Effect     `json:"latency"`
	Failures        *Effect     `json:"failures"`
	Verdict         string      `json:"verdict"`
}

type WindowInfo struct {
	Role        string            `json:"role"`
	Start       time.Time         `json:"start"`
	End         time.Time         `json:"end"`
	MeasuringMS int64             `json:"measuringMs"`
	Conditions  []store.Condition `json:"conditions"`
	NetAtStart  *netctx.Context   `json:"netAtStart"`
	NetChanges  int               `json:"netChanges"`
}

type Finding struct {
	Text   string   `json:"text"`
	Inputs []string `json:"inputs"`
}

type Result struct {
	RuleVersion  string            `json:"ruleVersion"`
	Verdict      string            `json:"verdict"`
	Windows      []WindowInfo      `json:"windows"`
	Probes       []ProbeComparison `json:"probes"`
	Findings     []Finding         `json:"findings"`
	Alternatives []string          `json:"alternatives"`
	NextTest     string            `json:"nextTest"`
	Caveats      []string          `json:"caveats"`
}

type series struct {
	probe    store.ObservationRow
	interval time.Duration
	okDur    []float64
	failInd  []float64
}

func collect(rows []store.ObservationRow) map[string]*series {
	out := map[string]*series{}
	for _, r := range rows {
		if r.Status == "cancelled" {
			continue
		}
		s := out[r.ProbeDefID]
		if s == nil {
			s = &series{probe: r, interval: r.Interval}
			out[r.ProbeDefID] = s
		}
		if r.Status == "ok" {
			s.okDur = append(s.okDur, float64(r.Duration.Microseconds()))
			s.failInd = append(s.failInd, 0)
		} else {
			s.failInd = append(s.failInd, 1)
		}
	}
	return out
}

func stats(s *series, dur time.Duration) WindowStats {
	if s == nil {
		return WindowStats{}
	}
	ws := WindowStats{N: len(s.failInd), OK: len(s.okDur), FailRate: mean(s.failInd)}
	if len(s.okDur) > 0 {
		m := int64(median(s.okDur))
		ws.P50US = &m
	}
	if exp := float64(dur) / float64(s.interval); exp > 0 {
		ws.Coverage = math.Min(1, float64(ws.N)/exp)
	}
	return ws
}

func Compute(ctx context.Context, st *store.Store, e store.Experiment) (Result, error) {
	res := Result{RuleVersion: RuleVersion, Probes: []ProbeComparison{}, Findings: []Finding{}, Caveats: []string{}}
	wa, wb := windowByRole(e, "baseline"), windowByRole(e, "intervention")
	if wa == nil || wb == nil {
		return res, fmt.Errorf("experimento sem as duas janelas")
	}
	seed := fnv.New64a()
	seed.Write([]byte(e.ID))
	rng := rand.New(rand.NewPCG(seed.Sum64(), 0x9e3779b97f4a7c15))

	var infos []WindowInfo
	var sets []map[string]*series
	var durs []time.Duration
	for _, w := range []*store.Window{wa, wb} {
		from, to := w.Start, windowEnd(*w)
		info := WindowInfo{Role: w.Role, Start: from, End: to, Conditions: w.Conditions}
		ivs, err := st.Intervals(ctx, from, to)
		if err != nil {
			return res, err
		}
		var measuring time.Duration
		for _, iv := range ivs {
			if iv.State == "measuring" {
				measuring += iv.Duration()
			}
		}
		info.MeasuringMS = measuring.Milliseconds()
		if info.NetAtStart, info.NetChanges, err = netDuring(ctx, st, from, to); err != nil {
			return res, err
		}
		rows, err := st.ObservationsBetween(ctx, from, to)
		if err != nil {
			return res, err
		}
		infos = append(infos, info)
		sets = append(sets, collect(rows))
		durs = append(durs, to.Sub(from))
	}
	res.Windows = infos

	idSet := map[string]bool{}
	for _, m := range sets {
		for id := range m {
			idSet[id] = true
		}
	}
	idsSorted := make([]string, 0, len(idSet))
	for id := range idSet {
		idsSorted = append(idsSorted, id)
	}
	sort.Strings(idsSorted)

	for _, id := range idsSorted {
		sa, sb := sets[0][id], sets[1][id]
		ref := sa
		if ref == nil {
			ref = sb
		}
		pc := ProbeComparison{ProbeDefID: id, Endpoint: ref.probe.EndpointLabel, Kind: ref.probe.Kind,
			ProtocolVersion: ref.probe.ProtocolVersion, Reasons: []string{},
			A: stats(sa, durs[0]), B: stats(sb, durs[1])}
		switch {
		case sa == nil:
			pc.Reasons = append(pc.Reasons, "a sonda não rodou na janela A (definição diferente ou ausente)")
		case sb == nil:
			pc.Reasons = append(pc.Reasons, "a sonda não rodou na janela B (definição diferente ou ausente)")
		}
		for i, d := range durs {
			if d < MinWindow {
				pc.Reasons = append(pc.Reasons, fmt.Sprintf("janela %s com %s, abaixo do mínimo de %s", "AB"[i:i+1],
					d.Round(time.Second), MinWindow))
			}
		}
		if sa != nil && pc.A.Coverage < minCoverage {
			pc.Reasons = append(pc.Reasons, fmt.Sprintf("cobertura de %.0f%% na janela A (mínimo 80%%)", pc.A.Coverage*100))
		}
		if sb != nil && pc.B.Coverage < minCoverage {
			pc.Reasons = append(pc.Reasons, fmt.Sprintf("cobertura de %.0f%% na janela B (mínimo 80%%)", pc.B.Coverage*100))
		}
		pc.Comparable = len(pc.Reasons) == 0
		if !pc.Comparable {
			pc.Verdict = NotComparable
			res.Probes = append(res.Probes, pc)
			continue
		}

		fa, fb := mean(sa.failInd), mean(sb.failInd)
		lo, hi := bootstrapDiff(rng, sa.failInd, sb.failInd, mean)
		pc.Failures = &Effect{Estimate: fb - fa, CILow: lo, CIHigh: hi, Threshold: failureThreshold}
		pc.Failures.Verdict = judge(*pc.Failures)

		if len(sa.okDur) >= minOKForLatency && len(sb.okDur) >= minOKForLatency {
			ma, mb := median(sa.okDur), median(sb.okDur)
			lo, hi := bootstrapDiff(rng, sa.okDur, sb.okDur, median)
			pc.Latency = &Effect{Estimate: mb - ma, CILow: lo, CIHigh: hi,
				Threshold: math.Max(latencyFloorUS, latencyRelative*ma)}
			pc.Latency.Verdict = judge(*pc.Latency)
		} else {
			pc.Latency = &Effect{Verdict: Insufficient}
		}
		pc.Verdict = combine(pc.Latency.Verdict, pc.Failures.Verdict)
		res.Probes = append(res.Probes, pc)
	}

	interpret(&res, e)
	return res, nil
}

func judge(ef Effect) string {
	switch {
	case ef.CIHigh < 0 && ef.Estimate <= -ef.Threshold:
		return Better
	case ef.CILow > 0 && ef.Estimate >= ef.Threshold:
		return Worse
	case ef.CILow >= -ef.Threshold && ef.CIHigh <= ef.Threshold:
		return NoDifference
	}
	return Inconclusive
}

func combine(lat, fail string) string {
	vs := []string{lat, fail}
	has := func(v string) bool { return vs[0] == v || vs[1] == v }
	switch {
	case has(Better) && has(Worse):
		return Inconclusive
	case has(Better) && !has(Inconclusive):
		return Better
	case has(Worse) && !has(Inconclusive):
		return Worse
	case lat == NoDifference && fail == NoDifference:
		return NoDifference
	case lat == Insufficient && fail == NoDifference:
		return NoDifference
	}
	return Inconclusive
}

func bootstrapDiff(rng *rand.Rand, a, b []float64, stat func([]float64) float64) (float64, float64) {
	diffs := make([]float64, bootstrapRounds)
	bufA, bufB := make([]float64, len(a)), make([]float64, len(b))
	for i := range diffs {
		sa := stat(resampleBlocks(rng, a, bufA))
		diffs[i] = stat(resampleBlocks(rng, b, bufB)) - sa
	}
	sort.Float64s(diffs)
	return diffs[int(0.025*float64(len(diffs)))], diffs[int(math.Ceil(0.975*float64(len(diffs))))-1]
}

func resampleBlocks(rng *rand.Rand, x, out []float64) []float64 {
	n := len(x)
	if n == 0 {
		return out[:0]
	}
	l := max(1, int(math.Round(math.Sqrt(float64(n)))))
	out = out[:0]
	for len(out) < n {
		start := rng.IntN(n - l + 1)
		out = append(out, x[start:start+l]...)
	}
	return out[:n]
}

func mean(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	s := 0.0
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

func median(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	c := append([]float64(nil), x...)
	sort.Float64s(c)
	m := len(c) / 2
	if len(c)%2 == 1 {
		return c[m]
	}
	return (c[m-1] + c[m]) / 2
}

func netDuring(ctx context.Context, st *store.Store, from, to time.Time) (*netctx.Context, int, error) {
	evs, err := st.SystemEvents(ctx, from.Add(-24*time.Hour), to)
	if err != nil {
		return nil, 0, err
	}
	var atStart *netctx.Context
	changes := 0
	for _, e := range evs {
		if e.Kind != "net_context" && e.Kind != "net_change" {
			continue
		}
		var c netctx.Context
		if json.Unmarshal([]byte(e.Detail), &c) != nil {
			continue
		}
		if !e.At.After(from) {
			atStart = &c
		} else if e.Kind == "net_change" {
			changes++
		}
	}
	return atStart, changes, nil
}

var kindLabel = map[string]string{"tcp": "TCP", "dns": "DNS", "https": "HTTPS"}
var netLabel = map[string]string{
	netctx.KindWiFi: "Wi-Fi", netctx.KindEthernet: "cabo (Ethernet)", netctx.KindVPN: "VPN",
	netctx.KindOther: "outra interface", netctx.KindUnknown: "desconhecida",
}

func name(p ProbeComparison) string { return fmt.Sprintf("%s (%s)", kindLabel[p.Kind], p.Endpoint) }

func decimal(s string) string { return strings.Replace(s, ".", ",", 1) }

func usText(v float64) string {
	a := math.Abs(v)
	if a < 1000 {
		return fmt.Sprintf("%.0f µs", v)
	}
	return decimal(fmt.Sprintf("%.2f ms", v/1000))
}

func ppText(v float64) string { return decimal(fmt.Sprintf("%+.1f", v*100)) + " p.p." }

func pctText(v float64) string { return decimal(fmt.Sprintf("%.1f%%", v*100)) }

func interpret(r *Result, e store.Experiment) {
	var comparable []ProbeComparison
	counts := map[string]int{}
	for _, p := range r.Probes {
		if !p.Comparable {
			r.Findings = append(r.Findings, Finding{
				Text:   fmt.Sprintf("%s não foi comparada: %s.", name(p), strings.Join(p.Reasons, "; ")),
				Inputs: []string{fmt.Sprintf("A: %d execuções; B: %d execuções", p.A.N, p.B.N)},
			})
			continue
		}
		comparable = append(comparable, p)
		counts[p.Verdict]++
		if p.Latency != nil && p.Latency.Verdict != Insufficient {
			l := p.Latency
			r.Findings = append(r.Findings, Finding{
				Text: fmt.Sprintf("%s: mediana %s em A e %s em B (diferença %s; intervalo de 95%%: %s a %s).",
					name(p), usText(float64(*p.A.P50US)), usText(float64(*p.B.P50US)), usText(l.Estimate),
					usText(l.CILow), usText(l.CIHigh)),
				Inputs: []string{fmt.Sprintf("A: %d ok de %d", p.A.OK, p.A.N), fmt.Sprintf("B: %d ok de %d", p.B.OK, p.B.N),
					fmt.Sprintf("limiar prático: %s", usText(l.Threshold))},
			})
		}
		if f := p.Failures; f != nil && (p.A.FailRate > 0 || p.B.FailRate > 0) {
			r.Findings = append(r.Findings, Finding{
				Text: fmt.Sprintf("%s: falhas em %s das execuções em A e %s em B (diferença %s; intervalo: %s a %s).",
					name(p), pctText(p.A.FailRate), pctText(p.B.FailRate), ppText(f.Estimate), ppText(f.CILow), ppText(f.CIHigh)),
				Inputs: []string{fmt.Sprintf("A: %d execuções", p.A.N), fmt.Sprintf("B: %d execuções", p.B.N)},
			})
		}
	}

	switch {
	case len(comparable) == 0:
		r.Verdict = NotComparable
	case counts[Better] > 0 && counts[Worse] > 0:
		r.Verdict = Inconclusive
	case counts[Better] > 0 && counts[Inconclusive] == 0:
		r.Verdict = Better
	case counts[Worse] > 0 && counts[Inconclusive] == 0:
		r.Verdict = Worse
	case counts[NoDifference] == len(comparable):
		r.Verdict = NoDifference
	default:
		r.Verdict = Inconclusive
	}

	a, b := r.Windows[0], r.Windows[1]
	if gap := b.Start.Sub(a.End); gap > maxGap {
		r.Caveats = append(r.Caveats, fmt.Sprintf("As janelas estão separadas por %s; a diferença pode ser do horário, não da mudança.", gap.Round(time.Minute)))
	}
	for _, w := range r.Windows {
		role := map[string]string{"baseline": "A", "intervention": "B"}[w.Role]
		if w.NetChanges > 0 {
			r.Caveats = append(r.Caveats, fmt.Sprintf("A interface de rede mudou %d vez(es) durante a janela %s.", w.NetChanges, role))
		}
		if w.NetAtStart != nil && w.NetAtStart.Kind == netctx.KindVPN {
			r.Caveats = append(r.Caveats, fmt.Sprintf("Havia VPN ativa no início da janela %s; a rota medida não é a mesma do uso sem VPN.", role))
		}
	}
	if a.NetAtStart != nil && b.NetAtStart != nil {
		if a.NetAtStart.Kind == b.NetAtStart.Kind && a.NetAtStart.Kind != netctx.KindUnknown {
			r.Caveats = append(r.Caveats, fmt.Sprintf("A interface detectada foi a mesma nas duas janelas (%s, %s). A mudança declarada (%q) não foi confirmada pela detecção.",
				netLabel[a.NetAtStart.Kind], a.NetAtStart.Interface, e.Intervention))
		} else if a.NetAtStart.Kind != netctx.KindUnknown && b.NetAtStart.Kind != netctx.KindUnknown {
			r.Findings = append(r.Findings, Finding{
				Text: fmt.Sprintf("Interface detectada: %s (%s) em A e %s (%s) em B.", netLabel[a.NetAtStart.Kind], a.NetAtStart.Interface,
					netLabel[b.NetAtStart.Kind], b.NetAtStart.Interface),
				Inputs: []string{"detecção: " + a.NetAtStart.Method},
			})
		}
	} else {
		r.Caveats = append(r.Caveats, "A interface de rede não foi detectada em pelo menos uma janela; a condição é só a declarada.")
	}
	r.Caveats = append(r.Caveats, "A janela B veio depois da A: mudança e horário se confundem numa única rodada.")

	r.Alternatives = []string{
		"Outras pessoas ou dispositivos usando a rede em só uma das janelas.",
		"Variação natural da rede ao longo do tempo, sem relação com a mudança.",
	}
	if len(comparable) == 0 {
		r.NextTest = "Repita a comparação com o agente medindo as duas janelas inteiras (pelo menos 10 min cada, sem pausar)."
		return
	}
	switch r.Verdict {
	case Better, Worse:
		r.NextTest = "Confirme repetindo na ordem inversa (B depois A) ou alternando A-B-A; se o resultado se mantiver, ele não é efeito do horário."
	default:
		r.NextTest = "Repita com janelas mais longas ou alternando A-B-A; com a variação atual, uma rodada não basta para separar a mudança do acaso."
	}
}

func Load(ctx context.Context, st *store.Store, e store.Experiment, now time.Time) (*Result, error) {
	if Phase(e, now) != PhaseDone {
		return nil, nil
	}
	if body, ok, err := st.ComparisonResult(ctx, e.ID, RuleVersion); err != nil {
		return nil, err
	} else if ok {
		var r Result
		if json.Unmarshal([]byte(body), &r) == nil {
			return &r, nil
		}
	}
	r, err := Compute(ctx, st, e)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(r)
	return &r, st.SaveComparisonResult(ctx, e.ID, RuleVersion, r.Verdict, string(body), now)
}
