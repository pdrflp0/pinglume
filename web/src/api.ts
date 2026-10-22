// Cliente da API local do agente. Mesma origem: o cookie de sessão vai junto
// automaticamente e não há CORS.

export type CoverageState =
  | "measuring"
  | "paused"
  | "suspended"
  | "agent_down"
  | "probe_unavailable"
  | "shutting_down"
  | "unrecorded";

export interface Gap {
  start: string;
  end: string;
  state: CoverageState;
  reason: string;
}

export interface Probe {
  probeDefId: string;
  endpoint: string;
  kind: string;
  protocolVersion: number;
  target: string;
  intervalMs: number;
  total: number;
  ok: number;
  failures: Record<string, number>;
  p50Us: number | null;
  p95Us: number | null;
  coverage: number | null;
  /** Tempo com intervalo ampliado após falhas seguidas. */
  backoffMs: number;
  lastStarted: string;
}

export interface Status {
  agentVersion: string;
  state: CoverageState;
  now: string;
  summary: {
    from: string;
    to: string;
    stateMs: Partial<Record<CoverageState, number>>;
    gaps: Gap[];
    probes: Probe[];
  };
}

export interface Bucket {
  t: number; // início do balde, ms desde a época
  n: number;
  ok: number;
  fail?: Record<string, number>;
  p50Us: number | null;
  p95Us: number | null;
  maxUs: number | null;
}

export interface Series {
  probeDefId: string;
  endpoint: string;
  kind: string;
  protocolVersion: number;
  target: string;
  intervalMs: number;
  buckets: Bucket[];
  backoff: { start: string; end: string; maxMultiplier: number }[];
}

export interface CoverageInterval {
  start: string;
  end: string;
  state: CoverageState;
  reason: string;
  open: boolean;
}

export interface Timeline {
  from: string;
  to: string;
  bucketMs: number;
  series: Series[];
  coverage: CoverageInterval[];
  events: { at: string; kind: string; detail: string }[];
}

export type Symptom = "audio_cut" | "video_freeze" | "dropped_call" | "slow" | "other";
export type Verdict = "aguardando_dados" | "sem_cobertura" | "degradacao_observada" | "sem_degradacao_detectavel";

export interface Incident {
  id: string;
  reportedAt: string;
  createdAt: string;
  symptom: Symptom;
  note: string;
  createdVia: "dashboard" | "cli";
}

export interface IncidentListItem extends Incident {
  verdict: Verdict;
  final: boolean;
}

export interface ProbeResult {
  probeDefId: string;
  endpoint: string;
  kind: string;
  protocolVersion: number;
  window: { n: number; ok: number; failures: Record<string, number>; p50Us: number | null };
  baseline: { n: number; ok: number; failRate: number; p50Us: number | null; p95Us: number | null };
  coverage: number | null;
  failing: boolean;
  slow: boolean;
  noBaseline: boolean;
}

export interface Analysis {
  ruleVersion: string;
  verdict: Verdict;
  windowFrom: string;
  windowTo: string;
  baselineFrom: string;
  measuringMs: number;
  windowMs: number;
  probes: ProbeResult[];
  findings: { text: string; probeDefId?: string; inputs: string[] }[];
  alternatives: string[];
  nextTest: string;
  caveats: string[];
  final: boolean;
}

export interface IncidentDetail {
  incident: Incident;
  analysis: Analysis;
}

/** Intervalo pedido à API: relativo a agora ou absoluto. */
export type RangeQuery = { window: string } | { from: Date; to: Date };

function rangeParams(r: RangeQuery): string {
  return "window" in r
    ? `window=${encodeURIComponent(r.window)}`
    : `from=${encodeURIComponent(r.from.toISOString())}&to=${encodeURIComponent(r.to.toISOString())}`;
}

/** Por que uma chamada falhou, do ponto de vista de quem usa o dashboard. */
export type FailureKind = "unauthenticated" | "offline" | "error";

export class ApiError extends Error {
  constructor(
    public kind: FailureKind,
    message: string,
  ) {
    super(message);
  }
}

async function call<T>(method: "GET" | "POST" | "DELETE", path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (body !== undefined) headers["Content-Type"] = "application/json";
    res = await fetch(path, {
      method,
      credentials: "same-origin",
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError("offline", "O agente não respondeu. Ele pode ter sido encerrado.");
  }
  if (res.status === 401) {
    throw new ApiError("unauthenticated", "Sessão ausente ou expirada.");
  }
  if (!res.ok) {
    let msg = `O agente respondeu ${res.status}.`;
    try {
      const body = (await res.json()) as { error?: string };
      if (body.error) msg = body.error;
    } catch {
      // corpo não é JSON: mantém a mensagem genérica
    }
    // Proxy de desenvolvimento sem agente atrás devolve 502/504.
    throw new ApiError(res.status === 502 || res.status === 504 ? "offline" : "error", msg);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  status: (r: RangeQuery) => call<Status>("GET", `/api/v1/status?${rangeParams(r)}`),
  timeline: (r: RangeQuery, buckets: number) =>
    call<Timeline>("GET", `/api/v1/timeline?${rangeParams(r)}&buckets=${buckets}`),
  incidents: (r: RangeQuery) => call<IncidentListItem[]>("GET", `/api/v1/incidents?${rangeParams(r)}`),
  incident: (id: string) => call<IncidentDetail>("GET", `/api/v1/incidents/${encodeURIComponent(id)}`),
  createIncident: (body: { symptom: Symptom; note: string; at?: string }) =>
    call<IncidentDetail>("POST", "/api/v1/incidents", body),
  deleteIncident: (id: string) => call<void>("DELETE", `/api/v1/incidents/${encodeURIComponent(id)}`),
  pause: () => call<{ state: CoverageState }>("POST", "/api/v1/pause"),
  resume: () => call<{ state: CoverageState }>("POST", "/api/v1/resume"),
};
