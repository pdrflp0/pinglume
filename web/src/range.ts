import type { RangeQuery } from "./api";

export type ViewRange = { live: true; spanMs: number } | { live: false; from: number; to: number };

export const PRESETS = [
  { label: "15 min", spanMs: 15 * 60_000, window: "15m" },
  { label: "1 h", spanMs: 60 * 60_000, window: "1h" },
  { label: "6 h", spanMs: 6 * 3600_000, window: "6h" },
  { label: "24 h", spanMs: 24 * 3600_000, window: "24h" },
  { label: "7 dias", spanMs: 7 * 24 * 3600_000, window: "168h" },
] as const;

export const MIN_SPAN_MS = 60_000;
export const MAX_SPAN_MS = 8 * 24 * 3600_000;

export function resolve(r: ViewRange, now: number): { from: number; to: number } {
  return r.live ? { from: now - r.spanMs, to: now } : { from: r.from, to: r.to };
}

export function toQuery(r: ViewRange): RangeQuery {
  if (r.live) {
    const p = PRESETS.find((x) => x.spanMs === r.spanMs);
    return { window: p ? p.window : `${Math.round(r.spanMs / 1000)}s` };
  }
  return { from: new Date(r.from), to: new Date(r.to) };
}

export function zoom(r: ViewRange, now: number, factor: number, center?: number): ViewRange {
  const { from, to } = resolve(r, now);
  const span = clampSpan((to - from) * factor);
  const c = center ?? (from + to) / 2;
  const ratio = (c - from) / (to - from);
  let nf = c - span * ratio;
  let nt = nf + span;
  if (nt > now) {
    nt = now;
    nf = now - span;
  }
  return { live: false, from: nf, to: nt };
}

export function pan(r: ViewRange, now: number, fraction: number): ViewRange {
  const { from, to } = resolve(r, now);
  const span = to - from;
  let nt = to + span * fraction;
  if (nt > now) nt = now;
  return { live: false, from: nt - span, to: nt };
}

export function select(from: number, to: number, now: number): ViewRange {
  let a = Math.min(from, to);
  let b = Math.min(Math.max(from, to), now);
  if (b - a < MIN_SPAN_MS) {
    const c = (a + b) / 2;
    a = c - MIN_SPAN_MS / 2;
    b = c + MIN_SPAN_MS / 2;
    if (b > now) {
      b = now;
      a = now - MIN_SPAN_MS;
    }
  }
  return { live: false, from: a, to: b };
}

function clampSpan(ms: number): number {
  return Math.min(MAX_SPAN_MS, Math.max(MIN_SPAN_MS, ms));
}
