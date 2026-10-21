import type { CoverageState } from "./api";

export const stateLabel: Record<CoverageState, string> = {
  measuring: "Medindo",
  paused: "Pausado",
  suspended: "Computador suspenso",
  agent_down: "Agente parado",
  probe_unavailable: "Sonda indisponível",
  shutting_down: "Encerrando",
  unrecorded: "Sem registro",
};

/** Ordem fixa na barra e na legenda: medição primeiro, lacunas depois. */
export const stateOrder: CoverageState[] = [
  "measuring",
  "paused",
  "suspended",
  "agent_down",
  "probe_unavailable",
  "unrecorded",
];

export const failureLabel: Record<string, string> = {
  timeout: "tempo esgotado",
  refused: "conexão recusada",
  dns_error: "erro de DNS",
  tls_error: "erro de TLS",
  http_error: "erro HTTP",
  net_unreachable: "rede inalcançável",
  cancelled: "cancelada pelo agente",
  error: "outro erro",
};

export function duration(ms: number): string {
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `${m} min ${s % 60} s` : `${m} min`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

export function latency(us: number | null): string {
  if (us === null) return "—";
  if (us < 1000) return `${us} µs`;
  return `${(us / 1000).toFixed(us < 10_000 ? 2 : 1)} ms`;
}

export function percent(r: number | null): string {
  return r === null ? "—" : `${Math.round(r * 100)}%`;
}

const timeFmt = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

export function clock(iso: string): string {
  return timeFmt.format(new Date(iso));
}

/** O que a latência de cada tipo de sonda mede. */
export const kindMeasure: Record<string, string> = {
  tcp: "handshake TCP",
  dns: "resolução de nome único",
  https: "requisição HTTPS completa, com conexão nova",
};
