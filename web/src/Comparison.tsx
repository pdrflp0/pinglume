import { useCallback, useEffect, useState, type FormEvent } from "react";
import { api, type CompareResult, type Effect, type Experiment, type ExpWindow, type NetContext } from "./api";
import { clock, compareVerdictLabel, duration, latency, netLabel } from "./format";

const PRESETS = [
  { question: "O cabo melhora em relação ao Wi-Fi?", intervention: "trocar o Wi-Fi por cabo" },
  { question: "Ficar perto do roteador melhora?", intervention: "levar o computador para perto do roteador" },
  { question: "Outra rede melhora?", intervention: "trocar de rede (ex.: roteador do celular)" },
];
const POLL_MS = 5000;

function useExperiments() {
  const [list, setList] = useState<Experiment[] | null>(null);
  const [net, setNet] = useState<NetContext | null>(null);
  const [error, setError] = useState<string | null>(null);
  const refresh = useCallback(async () => {
    try {
      const [l, n] = await Promise.all([api.experiments(), api.network()]);
      setList(l);
      setNet(n);
      setError(null);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }, []);
  useEffect(() => {
    refresh();
    const t = setInterval(refresh, POLL_MS);
    return () => clearInterval(t);
  }, [refresh]);
  return { list, net, error, refresh };
}

function netText(n: NetContext | null): string {
  if (!n || !n.kind || n.kind === "unknown") return "não detectada";
  return `${netLabel[n.kind]} (${n.interface})`;
}

export function ComparisonCard() {
  const { list, net, error, refresh } = useExperiments();
  const [creating, setCreating] = useState(false);
  if (list === null) return null;
  const active = list.find((e) => e.phase !== "done" && e.phase !== "cancelled");
  const past = list.filter((e) => e !== active);

  return (
    <section aria-labelledby="cmp-title" className="card">
      <h2 id="cmp-title">Comparação guiada</h2>
      {error && <p className="banner banner-error">{error}</p>}
      {active ? (
        <ActiveExperiment e={active} net={net} onChange={refresh} />
      ) : creating ? (
        <NewExperimentForm
          net={net}
          onCancel={() => setCreating(false)}
          onCreated={() => {
            setCreating(false);
            refresh();
          }}
        />
      ) : (
        <div>
          <p className="muted">
            Meça 10 minutos como está, faça uma mudança (ex.: cabo no lugar do Wi-Fi) e meça mais 10. O Pinglume só
            compara sondas medidas igualmente nas duas janelas e diz quando o resultado é inconclusivo.
          </p>
          <button className="btn" onClick={() => setCreating(true)}>
            Nova comparação
          </button>
        </div>
      )}
      {past.length > 0 && (
        <details className="past">
          <summary>Comparações anteriores ({past.length})</summary>
          <ul className="incidents">
            {past.map((e) => (
              <li key={e.id}>
                <details>
                  <summary className="incident-row">
                    <span className="num">{clock(e.createdAt)}</span>
                    <span>{e.question}</span>
                    {e.phase === "cancelled" ? (
                      <span className="tag">Cancelada</span>
                    ) : (
                      e.result && <CompareTag verdict={e.result.verdict} />
                    )}
                  </summary>
                  {e.result && <ResultView e={e} r={e.result} />}
                </details>
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

function CompareTag({ verdict }: { verdict: string }) {
  return <span className={`tag cmp-${verdict}`}>{compareVerdictLabel[verdict] ?? verdict}</span>;
}

function NewExperimentForm({
  net,
  onCancel,
  onCreated,
}: {
  net: NetContext | null;
  onCancel: () => void;
  onCreated: () => void;
}) {
  const [preset, setPreset] = useState(0);
  const [question, setQuestion] = useState(PRESETS[0].question);
  const [intervention, setIntervention] = useState(PRESETS[0].intervention);
  const [minutes, setMinutes] = useState(10);
  const [condition, setCondition] = useState(net?.kind === "wifi" ? "Wi-Fi" : "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  function pick(i: number) {
    setPreset(i);
    if (i < PRESETS.length) {
      setQuestion(PRESETS[i].question);
      setIntervention(PRESETS[i].intervention);
    }
  }

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.createExperiment({ question, intervention, windowMinutes: minutes, condition });
      onCreated();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form onSubmit={submit} className="cmp-form">
      <fieldset className="choices one-col">
        <legend>O que você quer testar?</legend>
        {PRESETS.map((p, i) => (
          <label key={p.question}>
            <input type="radio" name="preset" checked={preset === i} onChange={() => pick(i)} />
            {p.question}
          </label>
        ))}
        <label>
          <input type="radio" name="preset" checked={preset === PRESETS.length} onChange={() => pick(PRESETS.length)} />
          Outra pergunta
        </label>
      </fieldset>
      {preset === PRESETS.length && (
        <>
          <label className="field">
            Pergunta
            <input value={question} maxLength={200} onChange={(e) => setQuestion(e.target.value)} required />
          </label>
          <label className="field">
            Mudança que você vai fazer entre as janelas
            <input value={intervention} maxLength={200} onChange={(e) => setIntervention(e.target.value)} required />
          </label>
        </>
      )}
      <label className="field">
        Como está agora (janela A)
        <input
          value={condition}
          maxLength={100}
          onChange={(e) => setCondition(e.target.value)}
          placeholder="ex.: Wi-Fi, quarto"
        />
      </label>
      <p className="muted small">
        Interface detectada agora: <strong>{netText(net)}</strong>. Ela é registrada junto com o que você declarar.
      </p>
      <label className="field">
        Duração de cada janela
        <select value={minutes} onChange={(e) => setMinutes(Number(e.target.value))}>
          {[10, 15, 20, 30].map((m) => (
            <option key={m} value={m}>
              {m} minutos
            </option>
          ))}
        </select>
      </label>
      <p className="muted small">
        Mantenha o agente medindo e use a conexão como de costume durante as duas janelas. Pausar derruba a cobertura e
        pode tornar a comparação impossível.
      </p>
      {error && (
        <p className="banner banner-error" role="alert">
          {error}
        </p>
      )}
      <div className="dialog-actions">
        <button type="button" className="btn-ghost" onClick={onCancel}>
          Cancelar
        </button>
        <button type="submit" className="btn" disabled={busy}>
          {busy ? "Iniciando…" : "Começar janela A"}
        </button>
      </div>
    </form>
  );
}

function windowOf(e: Experiment, role: ExpWindow["role"]) {
  return e.windows.find((w) => w.role === role);
}

function Progress({ w, label }: { w: ExpWindow; label: string }) {
  const start = Date.parse(w.start);
  const end = Date.parse(w.plannedEnd);
  const now = Date.now();
  const left = Math.max(0, end - now);
  return (
    <div className="cmp-progress">
      <label>
        {label}: {left > 0 ? `faltam ${duration(left)}` : "concluída"}
        <progress max={end - start} value={Math.min(end - start, now - start)} />
      </label>
    </div>
  );
}

function Conditions({ w }: { w: ExpWindow }) {
  if (w.conditions.length === 0) return <span className="muted small">sem condição registrada</span>;
  return (
    <ul className="conds">
      {w.conditions.map((c) => (
        <li key={c.key + c.source}>
          {c.key}: <strong>{c.value}</strong>{" "}
          <span className="muted small">({c.source === "detected" ? "detectada" : "declarada"})</span>
        </li>
      ))}
    </ul>
  );
}

function ActiveExperiment({ e, net, onChange }: { e: Experiment; net: NetContext | null; onChange: () => void }) {
  const [condition, setCondition] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const a = windowOf(e, "baseline");
  const b = windowOf(e, "intervention");
  const aNet = a?.conditions.find((c) => c.source === "detected")?.value;
  const nowNet = net && net.kind && net.kind !== "unknown" ? `${net.kind} (${net.interface})` : undefined;

  async function act(f: () => Promise<unknown>) {
    setBusy(true);
    setError(null);
    try {
      await f();
      onChange();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div>
      <p>
        <strong>{e.question}</strong>
      </p>
      <ol className="steps">
        <li className={e.phase === "baseline_running" ? "current" : "done"}>
          Janela A, como está
          {a && <Conditions w={a} />}
          {a && e.phase === "baseline_running" && <Progress w={a} label="Janela A" />}
        </li>
        <li className={e.phase === "awaiting_intervention" ? "current" : b ? "done" : ""}>
          Faça a mudança: {e.intervention}
          {e.phase === "awaiting_intervention" && (
            <form
              className="cmp-form"
              onSubmit={(ev) => {
                ev.preventDefault();
                act(() => api.startIntervention(e.id, condition));
              }}
            >
              <p className="muted small">
                Interface detectada agora: <strong>{netText(net)}</strong>.
                {aNet && nowNet === aNet && " É a mesma da janela A; se a mudança envolve a conexão, confira antes de seguir."}
              </p>
              <label className="field">
                Como está agora (janela B)
                <input
                  value={condition}
                  maxLength={100}
                  onChange={(ev) => setCondition(ev.target.value)}
                  placeholder="ex.: cabo, mesmo cômodo"
                />
              </label>
              <button className="btn" disabled={busy}>
                Começar janela B
              </button>
            </form>
          )}
        </li>
        <li className={e.phase === "intervention_running" ? "current" : ""}>
          Janela B, depois da mudança
          {b && <Conditions w={b} />}
          {b && <Progress w={b} label="Janela B" />}
        </li>
      </ol>
      {error && (
        <p className="banner banner-error" role="alert">
          {error}
        </p>
      )}
      <div className="dialog-actions">
        <button className="btn-ghost" disabled={busy} onClick={() => act(() => api.cancelExperiment(e.id))}>
          Cancelar comparação
        </button>
      </div>
    </div>
  );
}

function effectText(ef: Effect | null, unit: "us" | "pp"): string {
  if (!ef || ef.verdict === "dados_insuficientes") return "dados insuficientes";
  const f = (v: number) => (unit === "us" ? latency(Math.round(v)) : `${v >= 0 ? "+" : ""}${(v * 100).toFixed(1).replace(".", ",")} p.p.`);
  return `${f(ef.estimate)} (${f(ef.ciLow)} a ${f(ef.ciHigh)})`;
}

export function ResultView({ e, r }: { e: Experiment; r: CompareResult }) {
  return (
    <div className="incident-detail">
      <p>
        Resultado: <CompareTag verdict={r.verdict} /> <span className="muted small">regra {r.ruleVersion}</span>
      </p>
      <p className="muted small">
        Janela A: {clock(r.windows[0].start)}–{clock(r.windows[0].end)} · Janela B: {clock(r.windows[1].start)}–
        {clock(r.windows[1].end)} · mudança declarada: {e.intervention}
      </p>
      <div className="table-wrap">
        <table>
          <caption className="sr-only">Comparação por sonda entre as janelas A e B</caption>
          <thead>
            <tr>
              <th scope="col">Sonda</th>
              <th scope="col" className="r">Mediana A</th>
              <th scope="col" className="r">Mediana B</th>
              <th scope="col">Diferença B − A (IC 95%)</th>
              <th scope="col">Falhas B − A</th>
              <th scope="col">Veredito</th>
            </tr>
          </thead>
          <tbody>
            {r.probes.map((p) => (
              <tr key={p.probeDefId}>
                <th scope="row">
                  {p.kind.toUpperCase()} <span className="muted small">{p.endpoint}</span>
                </th>
                <td className="r num">{latency(p.a.p50Us)}</td>
                <td className="r num">{latency(p.b.p50Us)}</td>
                <td className="num">{p.comparable ? effectText(p.latency, "us") : "—"}</td>
                <td className="num">
                  {!p.comparable ? "—" : p.a.failRate === 0 && p.b.failRate === 0 ? "sem falhas" : effectText(p.failures, "pp")}
                </td>
                <td>
                  <CompareTag verdict={p.verdict} />
                  {!p.comparable && <div className="muted small">{p.reasons.join("; ")}</div>}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted small">
        Diferença negativa = B mais rápido. "Melhora" ou "piora" só quando o intervalo exclui zero e a diferença passa de
        um limiar prático (10% da mediana de A ou 1 ms, o que for maior; 2 p.p. para falhas).
      </p>
      <h3>O que as medições mostram</h3>
      <ul className="findings">
        {r.findings.map((f, i) => (
          <li key={i}>
            {f.text}
            <div className="muted small">Base: {f.inputs.join("; ")}</div>
          </li>
        ))}
      </ul>
      <h3>Outras explicações possíveis</h3>
      <ul>
        {r.alternatives.map((x) => (
          <li key={x}>{x}</li>
        ))}
      </ul>
      <h3>Próximo teste</h3>
      <p>{r.nextTest}</p>
      {r.caveats.length > 0 && (
        <>
          <h3>Limites</h3>
          <ul className="muted small">
            {r.caveats.map((x) => (
              <li key={x}>{x}</li>
            ))}
          </ul>
        </>
      )}
    </div>
  );
}
