import type { CSSProperties } from "react";
import type { CoverageState, Probe, Status } from "./api";
import { clock, duration, kindName, latency, stateLabel } from "./format";
import { useCountUp } from "./useCountUp";

export interface Headline {
  measuredPct: number | null;
  measuredMs: number;
  totalMs: number;
  okPct: number | null;
  ok: number;
  total: number;
  slowestUs: number | null;
  slowestLabel: string;
  reports: number;
}

export function headline(status: Status, reports: number): Headline {
  const ms = status.summary.stateMs;
  const totalMs = Object.values(ms).reduce<number>((a, v) => a + (v ?? 0), 0);
  const measuredMs = ms.measuring ?? 0;
  const probes = status.summary.probes;
  const total = probes.reduce((a, p) => a + p.total, 0);
  const ok = probes.reduce((a, p) => a + p.ok, 0);
  let slowest: Probe | null = null;
  for (const p of probes) {
    if (p.p50Us !== null && (slowest === null || p.p50Us > (slowest.p50Us as number))) slowest = p;
  }
  return {
    measuredPct: totalMs > 0 ? (measuredMs / totalMs) * 100 : null,
    measuredMs,
    totalMs,
    okPct: total > 0 ? (ok / total) * 100 : null,
    ok,
    total,
    slowestUs: slowest?.p50Us ?? null,
    slowestLabel: slowest ? `${kindName(slowest.kind)} · ${slowest.endpoint}` : "sem medição no período",
    reports,
  };
}

const TRACE =
  "M0 40H58L66 40L73 24L81 58L90 8L99 70L107 40H196L203 40L209 31L216 47L222 40H318L326 40L333 24L341 58L350 8L359 70L367 40H456L463 40L469 31L476 47L482 40H600";

export function Pulse({ state }: { state: CoverageState | null }) {
  const live = state === "measuring";
  const d = live ? TRACE : "M0 40H600";
  return (
    <svg
      className={`pulse ${live ? "pulse-live" : state === null ? "pulse-unknown" : "pulse-idle"}`}
      viewBox="0 0 600 80"
      preserveAspectRatio="none"
      aria-hidden="true"
    >
      <path className="pulse-base" d="M0 40H600" />
      <path className="pulse-trace" pathLength={600} d={d} />
      {live && <path className="pulse-head" pathLength={600} d={d} />}
    </svg>
  );
}

export function StateChip({ state }: { state: CoverageState | null }) {
  if (state === null) {
    return <span className="chip chip-unknown">Estado desconhecido</span>;
  }
  return (
    <span className={`chip chip-${state}`}>
      <span className="chip-dot" aria-hidden="true" />
      {stateLabel[state]}
    </span>
  );
}

function Metric({
  label,
  value,
  format,
  hint,
  index,
  tone,
}: {
  label: string;
  value: number | null;
  format: (n: number) => string;
  hint: string;
  index: number;
  tone?: string;
}) {
  const shown = useCountUp(value);
  return (
    <div className="metric reveal" style={{ "--i": index + 2 } as CSSProperties}>
      <dt>{label}</dt>
      <dd className={`metric-value${tone ? ` ${tone}` : ""}`}>{shown === null ? "—" : format(shown)}</dd>
      <dd className="metric-hint">{hint}</dd>
    </div>
  );
}

interface HeroProps {
  status: Status;
  reports: number;
  offline: boolean;
}

export function Hero({ status, reports, offline }: HeroProps) {
  const h = headline(status, reports);
  const state = offline ? null : status.state;
  const word = offline ? "Sem contato" : stateLabel[status.state];
  const okTone = h.okPct === null ? "" : h.okPct >= 99 ? "tone-good" : h.okPct >= 90 ? "" : "tone-bad";
  return (
    <section className="hero card" aria-label="Resumo do agente">
      <div className="hero-state reveal" style={{ "--i": 1 } as CSSProperties}>
        <p className="eyebrow">
          Agente {status.agentVersion}
          {status.mode === "demo" ? " · simulação" : ""}
        </p>
        <p className={`hero-word state-${state ?? "unknown"}`}>{word}</p>
        <div className="hero-meta">
          <StateChip state={state} />
          <span className="num muted">leitura das {clock(status.now)}</span>
        </div>
        <Pulse state={state} />
      </div>
      <dl className="metrics">
        <Metric
          index={0}
          label="Tempo medindo"
          value={h.measuredPct}
          format={(n) => `${Math.round(n)}%`}
          hint={h.totalMs > 0 ? `${duration(h.measuredMs)} de ${duration(h.totalMs)}` : "sem registro no período"}
        />
        <Metric
          index={1}
          label="Execuções ok"
          value={h.okPct}
          format={(n) => `${n.toFixed(n >= 99.95 || n === 0 ? 0 : 1).replace(".", ",")}%`}
          hint={h.total > 0 ? `${h.ok} de ${h.total}` : "nenhuma execução"}
          tone={okTone}
        />
        <Metric
          index={2}
          label="Mediana mais alta"
          value={h.slowestUs}
          format={(n) => latency(Math.round(n))}
          hint={h.slowestLabel}
        />
        <Metric
          index={3}
          label="Relatos"
          value={h.reports}
          format={(n) => String(Math.round(n))}
          hint="no período exibido"
        />
      </dl>
    </section>
  );
}
