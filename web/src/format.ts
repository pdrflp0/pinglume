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
  if (h >= 48 && h % 24 === 0 && m % 60 === 0) return `${h / 24} dias`;
  return m % 60 ? `${h} h ${m % 60} min` : `${h} h`;
}

export function latency(us: number | null): string {
  if (us === null) return "—";
  const a = Math.abs(us);
  const sign = us < 0 ? "-" : "";
  if (a < 1000) return `${sign}${Math.round(a)} µs`;
  return `${sign}${(a / 1000).toFixed(a < 10_000 ? 2 : 1).replace(".", ",")} ms`;
}

export function percent(r: number | null): string {
  return r === null ? "—" : `${Math.round(r * 100)}%`;
}

const timeFmt = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

export function clock(iso: string): string {
  return timeFmt.format(new Date(iso));
}

export const kindMeasure: Record<string, string> = {
  tcp: "handshake TCP",
  dns: "resolução de nome único",
  https: "requisição HTTPS completa, com conexão nova",
};

export const symptomLabel: Record<string, string> = {
  audio_cut: "Áudio cortou",
  video_freeze: "Vídeo congelou",
  dropped_call: "Chamada caiu",
  slow: "Lentidão",
  other: "Outro",
};

export const verdictLabel: Record<string, string> = {
  aguardando_dados: "Aguardando dados",
  sem_cobertura: "Sem cobertura",
  degradacao_observada: "Degradação observada",
  sem_degradacao_detectavel: "Sem degradação detectável",
};

export const netLabel: Record<string, string> = {
  wifi: "Wi-Fi",
  ethernet: "Cabo (Ethernet)",
  vpn: "VPN",
  other: "Outra interface",
  unknown: "Não detectada",
  "": "Não detectada",
};

export const compareVerdictLabel: Record<string, string> = {
  melhora: "Melhora",
  piora: "Piora",
  sem_diferenca_detectavel: "Sem diferença detectável",
  inconclusivo: "Inconclusivo",
  nao_comparavel: "Não comparável",
  dados_insuficientes: "Dados insuficientes",
};

const dayTimeFmt = new Intl.DateTimeFormat("pt-BR", {
  day: "2-digit",
  month: "2-digit",
  hour: "2-digit",
  minute: "2-digit",
  second: "2-digit",
});

export function until(start: string | number, end: string | number): string {
  const a = new Date(start);
  const b = new Date(end);
  return a.toDateString() === b.toDateString() ? timeFmt.format(b) : dayTimeFmt.format(b);
}

export function megabytes(b: number): string {
  return `${(b / 1048576).toFixed(1).replace(".", ",")} MB`;
}
