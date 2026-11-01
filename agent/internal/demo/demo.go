package demo

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"sync"
	"time"

	"pinglume/agent/internal/ids"
	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

const (
	DeviceName = "pinglume-demo"
	seed       = 20261101
)

type probeSpec struct {
	kind     string
	interval time.Duration
	timeout  time.Duration
	base     time.Duration
	jitter   float64
	failWith []string
}

var specs = []probeSpec{
	{"tcp", 10 * time.Second, 3 * time.Second, 14 * time.Millisecond, 0.25, []string{"timeout", "refused"}},
	{"dns", 20 * time.Second, 3 * time.Second, 22 * time.Millisecond, 0.30, []string{"timeout", "dns_error"}},
	{"https", 30 * time.Second, 5 * time.Second, 55 * time.Millisecond, 0.25, []string{"timeout"}},
}

type phase struct {
	latency  float64
	jitter   float64
	failRate float64
}

var normal = phase{latency: 1, jitter: 1, failRate: 0.002}

type Scenario struct {
	T time.Time
}

func NewScenario(now time.Time) Scenario {
	return Scenario{T: now.Truncate(time.Minute)}
}

func (s Scenario) at(d time.Duration) time.Time { return s.T.Add(-d) }

func (s Scenario) phaseAt(t time.Time) phase {
	switch {
	case !t.Before(s.at(110*time.Minute)) && t.Before(s.at(106*time.Minute)):
		return phase{latency: 4, jitter: 2.5, failRate: 0.45}
	case !t.Before(s.at(90*time.Minute)) && t.Before(s.at(80*time.Minute)):
		return phase{latency: 1.8, jitter: 2.2, failRate: 0}
	case !t.Before(s.at(78*time.Minute)) && t.Before(s.at(68*time.Minute)):
		return phase{latency: 0.6, jitter: 0.4, failRate: 0}
	}
	return normal
}

func (s Scenario) measuring(t time.Time) bool {
	switch {
	case t.Before(s.at(180 * time.Minute)):
		return false
	case !t.Before(s.at(150*time.Minute)) && t.Before(s.at(145*time.Minute)):
		return false
	case !t.Before(s.at(50*time.Minute)) && t.Before(s.at(45*time.Minute)):
		return false
	}
	return t.Before(s.T)
}

func sample(rng *rand.Rand, sp probeSpec, ph phase) (string, time.Duration) {
	if rng.Float64() < ph.failRate {
		st := sp.failWith[rng.IntN(len(sp.failWith))]
		if st == "timeout" {
			return st, sp.timeout
		}
		return st, time.Duration(1+rng.IntN(5)) * time.Millisecond
	}
	j := 1 + sp.jitter*ph.jitter*(2*rng.Float64()-1)
	if j < 0.2 {
		j = 0.2
	}
	d := time.Duration(float64(sp.base) * ph.latency * j)
	if rng.Float64() < 0.01 {
		d *= 3
	}
	return "ok", d
}

func rngFor(kind string, salt uint64) *rand.Rand {
	h := fnv.New64a()
	h.Write([]byte(kind))
	return rand.New(rand.NewPCG(seed^salt, h.Sum64()))
}

func definitions(ctx context.Context, st *store.Store, now time.Time) (map[string]store.ProbeDefinition, error) {
	defs := map[string]store.ProbeDefinition{}
	for _, sp := range specs {
		d, err := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "demo-1", Host: "simulado", Owner: "demo"},
			store.ProbeDefinition{Kind: sp.kind, Target: "simulado://" + sp.kind, Interval: sp.interval, Timeout: sp.timeout,
				ProtocolVersion: 1, ParamsJSON: `{"simulated":true}`}, now)
		if err != nil {
			return nil, err
		}
		defs[sp.kind] = d
	}
	return defs, nil
}

func (s Scenario) Generate(ctx context.Context, st *store.Store, deviceID string) error {
	defs, err := definitions(ctx, st, s.T)
	if err != nil {
		return err
	}
	var intervals []store.Interval
	add := func(from, to time.Duration, state, reason string) {
		intervals = append(intervals, store.Interval{Start: s.at(from), End: s.at(to), State: state, Reason: reason, DetectedBy: "demo"})
	}
	add(180*time.Minute, 150*time.Minute, "measuring", "")
	add(150*time.Minute, 145*time.Minute, "agent_down", "simulação: agente desligado por 5 min")
	add(145*time.Minute, 50*time.Minute, "measuring", "")
	add(50*time.Minute, 45*time.Minute, "paused", "simulação: pausa pelo usuário")
	add(45*time.Minute, 0, "measuring", "")
	if _, err := st.Transition(ctx, deviceID, s.T, intervals, nil); err != nil {
		return err
	}

	for _, sp := range specs {
		rng := rngFor(sp.kind, 1)
		seq := int64(0)
		for t := s.at(180 * time.Minute); t.Before(s.T); t = t.Add(sp.interval) {
			if !s.measuring(t) {
				continue
			}
			status, dur := sample(rng, sp, s.phaseAt(t))
			seq++
			if err := st.InsertObservation(ctx, store.Observation{ID: ids.New(t), DeviceID: deviceID, ProbeDefID: defs[sp.kind].ID,
				Seq: seq, StartedWall: t, Duration: dur, Status: status, ReceivedAt: t}); err != nil {
				return err
			}
		}
	}

	for _, e := range []struct {
		at     time.Duration
		kind   string
		detail string
	}{
		{180 * time.Minute, "net_context", `{"interface":"demo0","kind":"wifi","method":"simulado"}`},
		{78 * time.Minute, "net_change", `{"interface":"demo1","kind":"ethernet","method":"simulado"}`},
		{68 * time.Minute, "net_change", `{"interface":"demo0","kind":"wifi","method":"simulado"}`},
	} {
		if err := st.InsertSystemEvent(ctx, s.at(e.at), e.kind, e.detail); err != nil {
			return err
		}
	}

	for _, inc := range []store.Incident{
		{ReportedAt: s.at(107 * time.Minute), Symptom: "audio_cut", Note: "simulação: a reunião travou e o áudio cortou"},
		{ReportedAt: s.at(35 * time.Minute), Symptom: "slow", Note: "simulação: lentidão percebida"},
	} {
		inc.CreatedAt, inc.CreatedVia = inc.ReportedAt, "dashboard"
		if _, err := st.InsertIncident(ctx, deviceID, inc); err != nil {
			return err
		}
	}

	e, err := st.InsertExperiment(ctx, deviceID, store.Experiment{Question: "O cabo melhora em relação ao Wi-Fi?",
		Intervention: "trocar o Wi-Fi por cabo", PlannedWindow: (10 * time.Minute).Milliseconds(), CreatedAt: s.at(90 * time.Minute)})
	if err != nil {
		return err
	}
	for _, w := range []store.Window{
		{Role: "baseline", Start: s.at(90 * time.Minute), PlannedEnd: s.at(80 * time.Minute), Conditions: []store.Condition{
			{Key: "conexão", Value: "Wi-Fi (simulado)", Source: "declared"},
			{Key: "interface", Value: "wifi (demo0)", Source: "detected", Method: "simulado"}}},
		{Role: "intervention", Start: s.at(78 * time.Minute), PlannedEnd: s.at(68 * time.Minute), Conditions: []store.Condition{
			{Key: "conexão", Value: "cabo (simulado)", Source: "declared"},
			{Key: "interface", Value: "ethernet (demo1)", Source: "detected", Method: "simulado"}}},
	} {
		if _, err := st.AddWindow(ctx, e.ID, w); err != nil {
			return err
		}
	}
	return nil
}

type Live struct {
	mu   sync.Mutex
	rng  *rand.Rand
	spec probeSpec
	now  func() time.Time
}

func (l *Live) Kind() string         { return l.spec.kind }
func (l *Live) ProtocolVersion() int { return 1 }

func BurstAt(t time.Time) bool {
	return t.Minute()%12 == 10
}

func (l *Live) Probe(ctx context.Context) probe.Result {
	if ctx.Err() != nil {
		return probe.Result{Status: "cancelled"}
	}
	ph := normal
	if BurstAt(l.now()) {
		ph = phase{latency: 3, jitter: 2, failRate: 0.5}
	}
	l.mu.Lock()
	status, dur := sample(l.rng, l.spec, ph)
	l.mu.Unlock()
	r := probe.Result{Status: status, Duration: dur}
	if status != "ok" {
		r.ErrorDetail = fmt.Sprintf("simulação: %s", status)
	}
	return r
}

func Jobs(ctx context.Context, st *store.Store, now func() time.Time) ([]sched.Job, error) {
	defs, err := definitions(ctx, st, now())
	if err != nil {
		return nil, err
	}
	var jobs []sched.Job
	for _, sp := range specs {
		jobs = append(jobs, sched.Job{Def: defs[sp.kind], Prober: &Live{rng: rngFor(sp.kind, 2), spec: sp, now: now}})
	}
	return jobs, nil
}

type Net struct{}

func (Net) Detect(context.Context) netctx.Context {
	return netctx.Context{Interface: "demo0", Kind: netctx.KindWiFi, Method: "simulado"}
}
