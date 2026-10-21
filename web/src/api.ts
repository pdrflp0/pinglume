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

async function call<T>(method: "GET" | "POST", path: string): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { method, credentials: "same-origin", headers: { Accept: "application/json" } });
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
  return (await res.json()) as T;
}

export const api = {
  status: (window: string) => call<Status>("GET", `/api/v1/status?window=${encodeURIComponent(window)}`),
  pause: () => call<{ state: CoverageState }>("POST", "/api/v1/pause"),
  resume: () => call<{ state: CoverageState }>("POST", "/api/v1/resume"),
};
