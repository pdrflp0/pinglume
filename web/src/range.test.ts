import { expect, test } from "vitest";
import { MIN_SPAN_MS, pan, select, zoom } from "./range";
import { coverageSegments } from "./Timeline";

const now = Date.parse("2026-10-21T12:00:00Z");
const live = { live: true as const, spanMs: 60 * 60_000 };

test("zoom aproxima em torno do centro e nunca passa de agora", () => {
  const r = zoom(live, now, 0.5);
  if (r.live) throw new Error("zoom deveria fixar o período");
  expect(r.to - r.from).toBe(30 * 60_000);
  const out = zoom(live, now, 2, now);
  if (out.live) throw new Error();
  expect(out.to).toBeLessThanOrEqual(now);
});

test("zoom respeita o mínimo de 1 min", () => {
  let r = zoom(live, now, 0.5);
  for (let i = 0; i < 20; i++) r = zoom(r, now, 0.5);
  if (r.live) throw new Error();
  expect(r.to - r.from).toBe(MIN_SPAN_MS);
});

test("deslocar para o futuro para em agora", () => {
  const r = pan(pan(live, now, -1), now, 5);
  if (r.live) throw new Error();
  expect(r.to).toBe(now);
  expect(r.to - r.from).toBe(60 * 60_000);
});

test("seleção curta vira 1 min e não passa de agora", () => {
  const r = select(now - 1000, now + 5000, now);
  if (r.live) throw new Error();
  expect(r.to).toBe(now);
  expect(r.to - r.from).toBe(MIN_SPAN_MS);
});

test("segmentos de cobertura preenchem buracos com 'sem registro'", () => {
  const from = Date.parse("2026-10-21T11:00:00Z");
  const to = Date.parse("2026-10-21T11:10:00Z");
  const segs = coverageSegments(
    [
      { start: "2026-10-21T10:55:00Z", end: "2026-10-21T11:02:00Z", state: "measuring", reason: "", open: false },
      { start: "2026-10-21T11:05:00Z", end: "2026-10-21T11:08:00Z", state: "paused", reason: "x", open: true },
    ],
    from,
    to,
  );
  expect(segs.map((s) => s.state)).toEqual(["measuring", "unrecorded", "paused", "unrecorded"]);
  expect(segs[0].start).toBe(from); // recortado ao período
  expect(segs.at(-1)!.end).toBe(to);
});
