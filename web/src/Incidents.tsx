import { useEffect, useRef, useState, type FormEvent } from "react";
import { api, type IncidentDetail, type IncidentListItem, type Symptom } from "./api";
import { clock, duration, symptomLabel, verdictLabel } from "./format";

const WHEN = [
  { label: "agora", ms: 0 },
  { label: "há 1 min", ms: 60_000 },
  { label: "há 2 min", ms: 120_000 },
  { label: "há 5 min", ms: 300_000 },
  { label: "há 10 min", ms: 600_000 },
];

/** Diálogo do relato. Usa <dialog> nativo: foco preso, Esc fecha. */
export function ReportDialog({
  open,
  onClose,
  onCreated,
}: {
  open: boolean;
  onClose: () => void;
  onCreated: (d: IncidentDetail) => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const [symptom, setSymptom] = useState<Symptom>("audio_cut");
  const [when, setWhen] = useState(0);
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) {
      setError(null);
      if (typeof d.showModal === "function") d.showModal();
      else d.setAttribute("open", "");
    } else if (!open && d.open) {
      if (typeof d.close === "function") d.close();
      else d.removeAttribute("open");
    }
  }, [open]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const at = when > 0 ? new Date(Date.now() - when).toISOString() : undefined;
      const d = await api.createIncident({ symptom, note, at });
      setNote("");
      setWhen(0);
      onCreated(d);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <dialog ref={ref} className="dialog" aria-labelledby="report-title" onClose={onClose} onCancel={onClose}>
      <form onSubmit={submit}>
        <h2 id="report-title">O que aconteceu?</h2>
        <p className="muted small">
          Isto registra um relato seu, não uma medição. O Pinglume compara o horário com as medições próximas.
        </p>
        <fieldset className="choices">
          <legend>Sintoma</legend>
          {(Object.keys(symptomLabel) as Symptom[]).map((s) => (
            <label key={s}>
              <input type="radio" name="symptom" checked={symptom === s} onChange={() => setSymptom(s)} />
              {symptomLabel[s]}
            </label>
          ))}
        </fieldset>
        <label className="field">
          Quando
          <select value={when} onChange={(e) => setWhen(Number(e.target.value))}>
            {WHEN.map((w) => (
              <option key={w.ms} value={w.ms}>
                {w.label}
              </option>
            ))}
          </select>
        </label>
        <label className="field">
          Observação (opcional)
          <textarea
            value={note}
            maxLength={500}
            rows={2}
            onChange={(e) => setNote(e.target.value)}
            placeholder="ex.: reunião no Meet, sala"
          />
        </label>
        <p className="muted small">A observação fica só neste computador.</p>
        {error && (
          <p className="banner banner-error" role="alert">
            {error}
          </p>
        )}
        <div className="dialog-actions">
          <button type="button" className="btn-ghost" onClick={onClose}>
            Cancelar
          </button>
          <button type="submit" className="btn" disabled={busy}>
            {busy ? "Registrando…" : "Registrar relato"}
          </button>
        </div>
      </form>
    </dialog>
  );
}

export function VerdictTag({ verdict }: { verdict: string }) {
  return <span className={`tag verdict-${verdict}`}>{verdictLabel[verdict] ?? verdict}</span>;
}

export function IncidentsCard({
  items,
  selected,
  onSelect,
  onDeleted,
}: {
  items: IncidentListItem[];
  selected: string | null;
  onSelect: (id: string | null) => void;
  onDeleted: () => void;
}) {
  return (
    <section aria-labelledby="inc-title" className="card">
      <h2 id="inc-title">Relatos</h2>
      {items.length === 0 ? (
        <p className="muted">
          Nenhum relato no período. Quando uma chamada travar, use <strong>Travou agora</strong> no topo da página.
        </p>
      ) : (
        <ul className="incidents">
          {items.map((i) => (
            <li key={i.id}>
              <button
                className={`incident-row${selected === i.id ? " on" : ""}`}
                aria-expanded={selected === i.id}
                onClick={() => onSelect(selected === i.id ? null : i.id)}
              >
                <span className="num">{clock(i.reportedAt)}</span>
                <span>{symptomLabel[i.symptom]}</span>
                <VerdictTag verdict={i.verdict} />
              </button>
              {selected === i.id && <IncidentDetailView id={i.id} onDeleted={onDeleted} />}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function IncidentDetailView({ id, onDeleted }: { id: string; onDeleted: () => void }) {
  const [detail, setDetail] = useState<IncidentDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);

  useEffect(() => {
    let alive = true;
    const load = () =>
      api
        .incident(id)
        .then((d) => alive && setDetail(d))
        .catch((e) => alive && setError(String(e.message ?? e)));
    load();
    // Enquanto a janela não fecha, a análise ainda muda.
    const t = setInterval(() => detail?.analysis.final || load(), 5000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [id, detail?.analysis.final]);

  async function remove() {
    try {
      await api.deleteIncident(id);
      onDeleted();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  if (error) return <p className="banner banner-error">{error}</p>;
  if (!detail) return <p className="muted small">Carregando análise…</p>;
  const a = detail.analysis;
  return (
    <div className="incident-detail">
      <p className="muted small">
        Relato às {clock(detail.incident.reportedAt)}
        {detail.incident.note && <> — “{detail.incident.note}”</>}. Medições analisadas: {clock(a.windowFrom)} –{" "}
        {clock(a.windowTo)} (agente medindo {duration(a.measuringMs)} de {duration(a.windowMs)}), comparadas com as 2 h
        anteriores. Regra {a.ruleVersion}.
      </p>
      <h3>O que as medições mostram</h3>
      <ul className="findings">
        {a.findings.map((f, i) => (
          <li key={i}>
            {f.text}
            <div className="muted small">Base: {f.inputs.join("; ")}</div>
          </li>
        ))}
      </ul>
      {a.alternatives.length > 0 && (
        <>
          <h3>Outras explicações possíveis</h3>
          <ul>
            {a.alternatives.map((x) => (
              <li key={x}>{x}</li>
            ))}
          </ul>
        </>
      )}
      {a.nextTest && (
        <>
          <h3>Próximo teste</h3>
          <p>{a.nextTest}</p>
        </>
      )}
      {a.caveats.length > 0 && (
        <>
          <h3>Limites</h3>
          <ul className="muted small">
            {a.caveats.map((x) => (
              <li key={x}>{x}</li>
            ))}
          </ul>
        </>
      )}
      <div className="dialog-actions">
        {confirming ? (
          <>
            <span className="small">Apagar este relato e sua análise? Não dá para desfazer.</span>
            <button className="btn-ghost" onClick={() => setConfirming(false)}>
              Manter
            </button>
            <button className="btn btn-danger" onClick={remove}>
              Apagar
            </button>
          </>
        ) : (
          <button className="btn-ghost" onClick={() => setConfirming(true)}>
            Apagar relato
          </button>
        )}
      </div>
    </div>
  );
}
