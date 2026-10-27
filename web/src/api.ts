

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

  backoffMs: number;
  approximate: boolean;
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
  t: number;
  n: number;
  ok: number;
  fail?: Record<string, number>;
  p50Us: number | null;
  p95Us: number | null;
  maxUs: number | null;
  approx?: boolean;
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

export interface NetContext {
  interface: string;
  kind: "wifi" | "ethernet" | "vpn" | "other" | "unknown" | "";
  method: string;
}

export interface Condition {
  key: string;
  value: string;
  source: "declared" | "detected";
  method?: string;
}

export interface ExpWindow {
  id: string;
  role: "baseline" | "intervention";
  start: string;
  plannedEnd: string;
  end: string | null;
  conditions: Condition[];
}

export type Phase = "baseline_running" | "awaiting_intervention" | "intervention_running" | "done" | "cancelled";
export type CompareVerdict =
  | "melhora"
  | "piora"
  | "sem_diferenca_detectavel"
  | "inconclusivo"
  | "nao_comparavel"
  | "dados_insuficientes";

export interface Effect {
  estimate: number;
  ciLow: number;
  ciHigh: number;
  threshold: number;
  verdict: CompareVerdict;
}

export interface ProbeComparison {
  probeDefId: string;
  endpoint: string;
  kind: string;
  protocolVersion: number;
  comparable: boolean;
  reasons: string[];
  a: { n: number; ok: number; failRate: number; p50Us: number | null; coverage: number };
  b: { n: number; ok: number; failRate: number; p50Us: number | null; coverage: number };
  latency: Effect | null;
  failures: Effect | null;
  verdict: CompareVerdict;
}

export interface CompareResult {
  ruleVersion: string;
  verdict: CompareVerdict;
  windows: { role: string; start: string; end: string; measuringMs: number; netChanges: number; netAtStart: NetContext | null }[];
  probes: ProbeComparison[];
  findings: { text: string; inputs: string[] }[];
  alternatives: string[];
  nextTest: string;
  caveats: string[];
}

export interface Experiment {
  id: string;
  question: string;
  intervention: string;
  plannedWindowMs: number;
  cancelledAt: string | null;
  createdAt: string;
  windows: ExpWindow[];
  phase: Phase;
  result: CompareResult | null;
}

export interface StorageInfo {
  usage: { usedBytes: number; fileBytes: number };
  limitBytes: number;
  rawRetentionMs: number;
  aggregateRetentionMs: number;
  oldestRaw: string | null;
  oldestAggregate: string | null;
}

export interface ExportMeta {
  id: string;
  createdAt: string;
  from: string;
  to: string;
  options: string;
  schemaVersion: string;
  agentVersion: string;
  sha256: string;
}

export interface ExportOptions {
  from: string;
  to: string;
  includeIncidents: boolean;
  includeComparisons: boolean;
  includeNotes: boolean;
}

export type RangeQuery = { window: string } | { from: Date; to: Date };

function rangeParams(r: RangeQuery): string {
  return "window" in r
    ? `window=${encodeURIComponent(r.window)}`
    : `from=${encodeURIComponent(r.from.toISOString())}&to=${encodeURIComponent(r.to.toISOString())}`;
}

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
    } catch {}

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
  network: () => call<NetContext>("GET", "/api/v1/network"),
  experiments: () => call<Experiment[]>("GET", "/api/v1/experiments"),
  createExperiment: (body: { question: string; intervention: string; windowMinutes: number; condition: string }) =>
    call<Experiment>("POST", "/api/v1/experiments", body),
  startIntervention: (id: string, condition: string) =>
    call<Experiment>("POST", `/api/v1/experiments/${encodeURIComponent(id)}/intervention`, { condition }),
  cancelExperiment: (id: string) => call<Experiment>("POST", `/api/v1/experiments/${encodeURIComponent(id)}/cancel`),
  storage: () => call<StorageInfo>("GET", "/api/v1/storage"),
  exports: () => call<ExportMeta[]>("GET", "/api/v1/exports"),
  createExport: (o: ExportOptions) => call<ExportMeta>("POST", "/api/v1/exports", o),
  pause: () => call<{ state: CoverageState }>("POST", "/api/v1/pause"),
  resume: () => call<{ state: CoverageState }>("POST", "/api/v1/resume"),
};
