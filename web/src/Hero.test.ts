import { expect, test } from "vitest";
import type { Probe, Status } from "./api";
import { headline } from "./Hero";

function probe(over: Partial<Probe>): Probe {
  return {
    probeDefId: "p",
    endpoint: "ep-1",
    kind: "tcp",
    protocolVersion: 1,
    target: "127.0.0.1:9443",
    intervalMs: 10_000,
    total: 0,
    ok: 0,
    failures: {},
    p50Us: null,
    p95Us: null,
    coverage: null,
    backoffMs: 0,
    approximate: false,
    lastStarted: "2026-11-08T12:00:00Z",
    ...over,
  };
}

function status(stateMs: Status["summary"]["stateMs"], probes: Probe[]): Status {
  return {
    agentVersion: "t",
    mode: "real",
    state: "measuring",
    now: "2026-11-08T12:00:00Z",
    summary: { from: "2026-11-08T11:45:00Z", to: "2026-11-08T12:00:00Z", stateMs, gaps: [], probes },
  };
}

test("agrega tempo medindo, execuções ok e a mediana mais alta", () => {
  const h = headline(
    status({ measuring: 600_000, agent_down: 120_000, unrecorded: 180_000 }, [
      probe({ total: 58, ok: 50, p50Us: 640 }),
      probe({ kind: "udp_echo", endpoint: "sp-1", total: 40, ok: 40, p50Us: 12_000 }),
    ]),
    3,
  );
  expect(h.measuredPct).toBeCloseTo(66.67, 1);
  expect(h.totalMs).toBe(900_000);
  expect(h.ok).toBe(90);
  expect(h.total).toBe(98);
  expect(h.okPct).toBeCloseTo(91.84, 1);
  expect(h.slowestUs).toBe(12_000);
  expect(h.slowestLabel).toBe("UDP · sp-1");
  expect(h.reports).toBe(3);
});

test("sem registro e sem execuções não inventa números", () => {
  const h = headline(status({}, [probe({ total: 0, ok: 0, p50Us: null })]), 0);
  expect(h.measuredPct).toBeNull();
  expect(h.okPct).toBeNull();
  expect(h.slowestUs).toBeNull();
  expect(h.slowestLabel).toBe("sem medição no período");
});
