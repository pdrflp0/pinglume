

const TIME_STEPS = [
  1_000, 5_000, 10_000, 30_000, 60_000, 5 * 60_000, 10 * 60_000, 15 * 60_000, 30 * 60_000, 3600_000,
  2 * 3600_000, 3 * 3600_000, 6 * 3600_000, 12 * 3600_000, 24 * 3600_000,
];

export function timeTicks(from: number, to: number, target = 6): number[] {
  const span = to - from;
  const step = TIME_STEPS.find((s) => span / s <= target) ?? TIME_STEPS[TIME_STEPS.length - 1];
  const offset = new Date(from).getTimezoneOffset() * 60_000;

  let t = Math.ceil((from - offset) / step) * step + offset;
  const out: number[] = [];
  for (; t <= to; t += step) out.push(t);
  return out;
}

const hm = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit" });
const hms = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });
const dhm = new Intl.DateTimeFormat("pt-BR", { day: "2-digit", month: "2-digit", hour: "2-digit", minute: "2-digit" });

export function tickLabel(t: number, spanMs: number): string {
  if (spanMs > 36 * 3600_000) return dhm.format(t);
  if (spanMs <= 5 * 60_000) return hms.format(t);
  return hm.format(t);
}

export function rangeLabel(from: number, to: number): string {
  const sameDay = new Date(from).toDateString() === new Date(to).toDateString();
  const today = new Date().toDateString() === new Date(to).toDateString();
  const f = to - from <= 5 * 60_000 ? hms : hm;
  if (sameDay && today) return `${f.format(from)} – ${f.format(to)}`;
  return `${dhm.format(from)} – ${dhm.format(to)}`;
}

export function niceMax(v: number): number {
  if (v <= 0) return 1;
  const exp = Math.pow(10, Math.floor(Math.log10(v)));
  for (const m of [1, 2, 5, 10]) if (v <= m * exp) return m * exp;
  return 10 * exp;
}
