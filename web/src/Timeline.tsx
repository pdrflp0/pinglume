import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import type { Bucket, CoverageState, IncidentListItem, Series, Timeline as TimelineData } from "./api";
import { duration, failureLabel, kindMeasure, latency, stateLabel, symptomLabel } from "./format";
import { niceMax, tickLabel, timeTicks } from "./scale";

const M = { left: 56, right: 14 };
const PLOT_H = 96;
const FAIL_H = 16;
const STRIP_H = 14;

export interface Segment {
  start: number;
  end: number;
  state: CoverageState;
  reason: string;
}

/**
 * Cobre [from, to] inteiro: intervalos registrados mais os buracos entre eles,
 * marcados como "sem registro". Nada fica implicitamente "ok".
 */
export function coverageSegments(cov: TimelineData["coverage"], from: number, to: number): Segment[] {
  const segs = cov
    .map((c) => ({
      start: Math.max(from, Date.parse(c.start)),
      end: Math.min(to, Date.parse(c.end)),
      state: c.state,
      reason: c.reason,
    }))
    .filter((s) => s.end > s.start)
    .sort((a, b) => a.start - b.start);
  const out: Segment[] = [];
  let cursor = from;
  for (const s of segs) {
    if (s.start > cursor) out.push({ start: cursor, end: s.start, state: "unrecorded", reason: "" });
    const start = Math.max(s.start, cursor);
    if (s.end > start) out.push({ ...s, start });
    cursor = Math.max(cursor, s.end);
  }
  if (cursor < to) out.push({ start: cursor, end: to, state: "unrecorded", reason: "" });
  return out;
}

/** Trechos contínuos de latência: quebra a linha em lacunas e saltos longos. */
function runs(s: Series, bucketMs: number, gaps: Segment[]): Bucket[][] {
  const maxJump = Math.max(bucketMs, s.intervalMs) * 2.5;
  const out: Bucket[][] = [];
  let cur: Bucket[] = [];
  let prev: Bucket | null = null;
  for (const b of s.buckets) {
    if (b.p50Us === null) {
      if (cur.length) out.push(cur);
      cur = [];
      prev = null;
      continue;
    }
    if (prev) {
      const a = prev.t + bucketMs / 2;
      const c = b.t + bucketMs / 2;
      const crossesGap = gaps.some((g) => g.start < c && g.end > a);
      if (c - a > maxJump || crossesGap) {
        out.push(cur);
        cur = [];
      }
    }
    cur.push(b);
    prev = b;
  }
  if (cur.length) out.push(cur);
  return out;
}

function failures(b: Bucket): number {
  return b.fail ? Object.values(b.fail).reduce((a, v) => a + v, 0) : 0;
}

interface Props {
  data: TimelineData;
  incidents: IncidentListItem[];
  onIncident: (id: string) => void;
  onSelect: (from: number, to: number) => void;
  onZoom: (factor: number, center?: number) => void;
  onPan: (fraction: number) => void;
  onLive: () => void;
}

export function Timeline({ data, incidents, onIncident, onSelect, onZoom, onPan, onLive }: Props) {
  const from = Date.parse(data.from);
  const to = Date.parse(data.to);
  const span = to - from;
  const bucketMs = data.bucketMs;

  const wrap = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(720);
  useEffect(() => {
    const el = wrap.current;
    if (!el || typeof ResizeObserver === "undefined") return; // sem suporte: largura padrão
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(280, Math.floor(e.contentRect.width))));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const plotW = width - M.left - M.right;
  const x = (t: number) => M.left + ((t - from) / span) * plotW;
  const tAt = (px: number) => from + ((px - M.left) / plotW) * span;

  const segs = useMemo(() => coverageSegments(data.coverage, from, to), [data.coverage, from, to]);
  const gaps = useMemo(() => segs.filter((s) => s.state !== "measuring"), [segs]);
  const yMax = useMemo(() => {
    let m = 0;
    for (const s of data.series) for (const b of s.buckets) if (b.p95Us !== null) m = Math.max(m, b.p95Us);
    return niceMax(m / 1000); // em ms
  }, [data.series]);
  const ticks = useMemo(() => timeTicks(from, to, Math.max(3, Math.floor(plotW / 110))), [from, to, plotW]);

  // Cursor: índice do balde sob o ponteiro ou o foco do teclado.
  const nBuckets = Math.max(1, Math.ceil(span / bucketMs));
  const [cursor, setCursor] = useState<number | null>(null);
  useEffect(() => setCursor((c) => (c === null ? null : Math.min(c, nBuckets - 1))), [nBuckets]);
  const cursorT = cursor === null ? null : from + cursor * bucketMs;
  const idxAt = (t: number) => Math.min(nBuckets - 1, Math.max(0, Math.floor((t - from) / bucketMs)));

  // Arrastar para aproximar.
  const [brush, setBrush] = useState<{ a: number; b: number } | null>(null);
  function localX(e: PointerEvent) {
    const r = e.currentTarget.getBoundingClientRect();
    return Math.min(M.left + plotW, Math.max(M.left, e.clientX - r.left));
  }
  function onPointerDown(e: PointerEvent<HTMLDivElement>) {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const px = localX(e);
    setBrush({ a: px, b: px });
  }
  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    const px = localX(e);
    setCursor(idxAt(tAt(px)));
    if (brush) setBrush({ a: brush.a, b: px });
  }
  function onPointerUp() {
    if (brush && Math.abs(brush.b - brush.a) > 6) onSelect(tAt(brush.a), tAt(brush.b));
    setBrush(null);
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    const last = nBuckets - 1;
    const c = cursor ?? last;
    const handled = () => e.preventDefault();
    switch (e.key) {
      case "ArrowLeft":
        handled();
        if (e.shiftKey) onPan(-0.25);
        else setCursor(Math.max(0, c - 1));
        break;
      case "ArrowRight":
        handled();
        if (e.shiftKey) onPan(0.25);
        else setCursor(Math.min(last, c + 1));
        break;
      case "Home":
        handled();
        setCursor(0);
        break;
      case "End":
        handled();
        setCursor(last);
        break;
      case "+":
      case "=":
        handled();
        onZoom(0.5, cursorT === null ? undefined : cursorT + bucketMs / 2);
        break;
      case "-":
      case "_":
        handled();
        onZoom(2, cursorT === null ? undefined : cursorT + bucketMs / 2);
        break;
      case "Escape":
        handled();
        setCursor(null);
        onLive();
        break;
    }
  }

  const strokeY = (us: number) => PLOT_H - Math.min(1, us / 1000 / yMax) * PLOT_H;
  const yTicks = [0, yMax / 2, yMax];
  const cursorX = cursorT === null ? null : x(cursorT + bucketMs / 2);
  // Relatos são marcados à parte das medições: losango acima da faixa de
  // cobertura e linha tracejada atravessando os painéis.
  const marks = incidents
    .map((i) => ({ ...i, t: Date.parse(i.reportedAt) }))
    .filter((i) => i.t >= from && i.t <= to);
  const markLines = (h: number) =>
    marks.map((m) => <line key={m.id} x1={x(m.t)} x2={x(m.t)} y1={0} y2={h} className="tl-incident" />);

  return (
    <div className="tl">
      <Readout data={data} cursorT={cursorT} segs={segs} marks={marks} />
      <div
        ref={wrap}
        className="tl-plot"
        tabIndex={0}
        role="group"
        aria-label="Linha do tempo das sondas"
        aria-describedby="tl-help"
        onKeyDown={onKeyDown}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerLeave={() => !brush && setCursor(null)}
      >
        <div className="tl-marks" style={{ height: marks.length ? 18 : 0 }}>
          {marks.map((m) => (
            <button
              key={m.id}
              className="tl-mark"
              style={{ left: x(m.t) }}
              onClick={() => onIncident(m.id)}
              onPointerDown={(e) => e.stopPropagation()}
              aria-label={`Relato: ${symptomLabel[m.symptom]} às ${bucketFmt.format(m.t)}`}
              title={`Relato: ${symptomLabel[m.symptom]}`}
            />
          ))}
        </div>
        <svg width={width} height={STRIP_H + 22} className="tl-svg" aria-hidden="true">
          <Patterns />
          <text x={M.left - 8} y={STRIP_H - 2} className="tl-axis-label" textAnchor="end">
            Cobertura
          </text>
          {segs.map((s) => (
            <rect
              key={`${s.start}-${s.state}`}
              x={x(s.start)}
              y={0}
              width={Math.max(1, x(s.end) - x(s.start) - 1)}
              height={STRIP_H}
              rx={2}
              className={`cov cov-${s.state}`}
            />
          ))}
          {markLines(STRIP_H)}
          {cursorX !== null && <line x1={cursorX} x2={cursorX} y1={0} y2={STRIP_H} className="tl-cursor" />}
        </svg>

        {data.series.length === 0 && (
          <p className="tl-empty muted">
            Nenhuma observação neste período. A faixa acima mostra se o agente estava medindo.
          </p>
        )}

        {data.series.map((s) => {
          const rs = runs(s, bucketMs, gaps);
          return (
            <div key={s.probeDefId} className="tl-panel">
              <div className="tl-title">
                <strong>{s.endpoint}</strong>
                <span className="muted">
                  {" "}
                  · {s.kind.toUpperCase()} v{s.protocolVersion} · {kindMeasure[s.kind] ?? s.kind} · {s.target} · a
                  cada {duration(s.intervalMs)}
                </span>
              </div>
              <svg width={width} height={PLOT_H + FAIL_H + 10} className="tl-svg" aria-hidden="true">
                {gaps.map((g) => (
                  <rect
                    key={`${g.start}-${g.state}`}
                    x={x(g.start)}
                    y={0}
                    width={Math.max(1, x(g.end) - x(g.start))}
                    height={PLOT_H}
                    className={`gapband cov-${g.state}`}
                  />
                ))}
                {s.backoff.map((b) => (
                  <rect
                    key={b.start}
                    x={x(Date.parse(b.start))}
                    y={0}
                    width={Math.max(2, x(Date.parse(b.end)) - x(Date.parse(b.start)))}
                    height={4}
                    className="tl-backoff"
                  />
                ))}
                {yTicks.map((v) => (
                  <g key={v}>
                    <line x1={M.left} x2={M.left + plotW} y1={strokeY(v * 1000)} y2={strokeY(v * 1000)} className="tl-grid" />
                    <text x={M.left - 8} y={strokeY(v * 1000) + 4} className="tl-axis-label" textAnchor="end">
                      {v === 0 ? "0" : `${formatMs(v)} ms`}
                    </text>
                  </g>
                ))}
                {rs.map((run) => {
                  const pts = run.map((b) => [x(b.t + bucketMs / 2), strokeY(b.p50Us!)] as const);
                  if (run.length === 1) {
                    return <circle key={run[0].t} cx={pts[0][0]} cy={pts[0][1]} r={3} className="tl-point" />;
                  }
                  const band =
                    run.map((b) => `${x(b.t + bucketMs / 2)},${strokeY(b.p95Us!)}`).join(" ") +
                    " " +
                    [...run]
                      .reverse()
                      .map((b) => `${x(b.t + bucketMs / 2)},${strokeY(b.p50Us!)}`)
                      .join(" ");
                  return (
                    <g key={run[0].t}>
                      <polygon points={band} className="tl-band" />
                      <polyline points={pts.map((p) => p.join(",")).join(" ")} className="tl-line" />
                    </g>
                  );
                })}
                <text x={M.left - 8} y={PLOT_H + 6 + FAIL_H - 3} className="tl-axis-label" textAnchor="end">
                  Falhas
                </text>
                <line x1={M.left} x2={M.left + plotW} y1={PLOT_H + 6 + FAIL_H} y2={PLOT_H + 6 + FAIL_H} className="tl-grid" />
                {s.buckets.map((b) => {
                  const f = failures(b);
                  if (f === 0) return null;
                  const h = Math.max(3, (f / b.n) * FAIL_H);
                  return (
                    <rect
                      key={b.t}
                      x={x(b.t)}
                      y={PLOT_H + 6 + FAIL_H - h}
                      width={Math.max(2, x(b.t + bucketMs) - x(b.t) - 1)}
                      height={h}
                      className="tl-fail"
                    />
                  );
                })}
                {markLines(PLOT_H + 6 + FAIL_H)}
                {cursorX !== null && (
                  <line x1={cursorX} x2={cursorX} y1={0} y2={PLOT_H + 6 + FAIL_H} className="tl-cursor" />
                )}
              </svg>
            </div>
          );
        })}

        <svg width={width} height={20} className="tl-svg" aria-hidden="true">
          {ticks.map((t) => (
            <text key={t} x={x(t)} y={14} className="tl-axis-label" textAnchor="middle">
              {tickLabel(t, span)}
            </text>
          ))}
        </svg>

        {brush && Math.abs(brush.b - brush.a) > 2 && (
          <div
            className="tl-brush"
            style={{ left: Math.min(brush.a, brush.b), width: Math.abs(brush.b - brush.a) }}
          />
        )}
      </div>
      <p id="tl-help" className="muted small">
        Linha: mediana do tempo da sonda (o título de cada painel diz o que ela mede); faixa clara: até o p95, só
        execuções bem-sucedidas. Barras de falha: fração das execuções que falharam no trecho. Traço tracejado no
        topo do painel: sonda em backoff (intervalo ampliado após falhas seguidas). Áreas hachuradas: sem medição.
        Losango acima da cobertura e linha tracejada: um relato seu (não é medição). Arraste para aproximar. No teclado: ←/→ percorre, Shift+←/→ desloca, + e − aproximam e afastam, Esc volta ao
        vivo.
      </p>
    </div>
  );
}

function formatMs(v: number): string {
  return v >= 10 ? String(Math.round(v)) : v >= 1 ? v.toFixed(1).replace(".0", "") : v.toFixed(2);
}

function Patterns() {
  // Hachuras para estados sem medição: a textura carrega o significado além da cor.
  const defs: [CoverageState, number, number][] = [
    ["paused", 90, 6],
    ["suspended", 45, 6],
    ["agent_down", 135, 6],
    ["probe_unavailable", 0, 5],
    ["unrecorded", 45, 4],
  ];
  return (
    <defs>
      {defs.map(([state, angle, size]) => (
        <pattern
          key={state}
          id={`hatch-${state}`}
          width={size}
          height={size}
          patternUnits="userSpaceOnUse"
          patternTransform={`rotate(${angle})`}
        >
          <rect width={state === "unrecorded" ? 1 : 2.5} height={size} className={`hatch hatch-${state}`} />
        </pattern>
      ))}
      <pattern id="backoff-dash" width={7} height={4} patternUnits="userSpaceOnUse">
        <rect width={4} height={4} className="hatch-backoff" />
      </pattern>
    </defs>
  );
}

const bucketFmt = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

function Readout({
  data,
  cursorT,
  segs,
  marks,
}: {
  data: TimelineData;
  cursorT: number | null;
  segs: Segment[];
  marks: (IncidentListItem & { t: number })[];
}) {
  if (cursorT === null) {
    return (
      <div className="tl-readout muted" aria-live="polite">
        Passe o ponteiro sobre o gráfico ou foque nele e use as setas para ler os valores de cada trecho.
      </div>
    );
  }
  const end = cursorT + data.bucketMs;
  const mid = cursorT + data.bucketMs / 2;
  const seg = segs.find((s) => s.start <= mid && s.end > mid);
  const here = marks.filter((m) => m.t >= cursorT && m.t < end);
  return (
    <div className="tl-readout" aria-live="polite">
      <div className="tl-readout-head">
        <strong className="num">
          {bucketFmt.format(cursorT)} – {bucketFmt.format(end)}
        </strong>
        {seg && <span className={`tag tag-${seg.state}`}>{stateLabel[seg.state]}</span>}
        {here.map((m) => (
          <span key={m.id} className="tag tag-incident">
            Relato: {symptomLabel[m.symptom]}
          </span>
        ))}
      </div>
      <ul>
        {data.series.map((s) => {
          const b = s.buckets.find((x) => x.t === cursorT);
          const inBackoff = s.backoff.some((k) => Date.parse(k.start) <= mid && Date.parse(k.end) > mid);
          return (
            <li key={s.probeDefId}>
              <span className="tl-readout-name">
                {s.endpoint} {s.kind.toUpperCase()}
              </span>
              {b ? (
                <span>
                  {b.n} {b.n === 1 ? "execução" : "execuções"}, {b.ok} ok
                  {b.fail &&
                    Object.entries(b.fail).map(([k, v]) => (
                      <span key={k}>
                        , {v} {failureLabel[k] ?? k}
                      </span>
                    ))}
                  {b.p50Us !== null && (
                    <>
                      {" "}
                      · mediana {latency(b.p50Us)} · p95 {latency(b.p95Us)}
                    </>
                  )}
                </span>
              ) : (
                <span className="muted">nenhuma execução começou neste trecho</span>
              )}
              {inBackoff && <span className="muted"> · em backoff</span>}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
