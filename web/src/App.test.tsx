import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { App } from "./App";
import type { Status, Timeline } from "./api";

const status: Status = {
  agentVersion: "0.1.0-dev",
  state: "measuring",
  now: "2026-10-20T21:15:00Z",
  summary: {
    from: "2026-10-20T21:00:00Z",
    to: "2026-10-20T21:15:00Z",
    stateMs: { measuring: 600_000, agent_down: 120_000, unrecorded: 180_000 },
    gaps: [
      {
        start: "2026-10-20T21:05:00Z",
        end: "2026-10-20T21:07:00Z",
        state: "agent_down",
        reason: "encerramento inesperado do agente",
      },
    ],
    probes: [
      {
        endpoint: "ep-1",
        kind: "tcp",
        protocolVersion: 1,
        target: "127.0.0.1:9443",
        intervalMs: 10_000,
        total: 58,
        ok: 50,
        failures: { refused: 8 },
        p50Us: 640,
        p95Us: 3100,
        coverage: 0.97,
        probeDefId: "p1",
        backoffMs: 0,
        lastStarted: "2026-10-20T21:14:55Z",
      },
    ],
  },
};

const timeline: Timeline = {
  from: "2026-10-20T21:00:00Z",
  to: "2026-10-20T21:15:00Z",
  bucketMs: 3000,
  series: [
    {
      probeDefId: "p1",
      endpoint: "ep-1",
      kind: "tcp",
      protocolVersion: 1,
      target: "127.0.0.1:9443",
      intervalMs: 10_000,
      buckets: [
        { t: Date.parse("2026-10-20T21:01:00Z"), n: 1, ok: 1, p50Us: 600, p95Us: 600, maxUs: 600 },
        { t: Date.parse("2026-10-20T21:01:09Z"), n: 1, ok: 0, fail: { refused: 1 }, p50Us: null, p95Us: null, maxUs: null },
      ],
      backoff: [],
    },
  ],
  coverage: [
    { start: "2026-10-20T21:00:00Z", end: "2026-10-20T21:05:00Z", state: "measuring", reason: "", open: false },
    {
      start: "2026-10-20T21:05:00Z",
      end: "2026-10-20T21:07:00Z",
      state: "agent_down",
      reason: "encerramento inesperado do agente",
      open: false,
    },
  ],
  events: [],
};

const detail = {
  incident: {
    id: "inc1",
    reportedAt: "2026-10-20T21:10:00Z",
    createdAt: "2026-10-20T21:10:00Z",
    symptom: "audio_cut",
    note: "",
    createdVia: "dashboard",
  },
  analysis: {
    ruleVersion: "incident-v1",
    verdict: "degradacao_observada",
    windowFrom: "2026-10-20T21:05:00Z",
    windowTo: "2026-10-20T21:11:00Z",
    baselineFrom: "2026-10-20T19:05:00Z",
    measuringMs: 360_000,
    windowMs: 360_000,
    probes: [],
    findings: [
      { text: "TCP (ep-1) falhou em 5 de 36 execuções na janela.", inputs: ["janela: 36 execuções"] },
      { text: "A degradação coincide com o relato no tempo. Isso não prova que ela causou o problema.", inputs: ["regra incident-v1"] },
    ],
    alternatives: ["Problema no trecho entre este computador e a internet."],
    nextTest: "Faça uma comparação guiada por cabo.",
    caveats: [],
    final: true,
  },
};

let incidents: unknown[] = [];

/** Responde por rota, como o agente. */
function route(url: string, init?: RequestInit) {
  if (url === "/api/v1/incidents" && init?.method === "POST") {
    incidents = [{ ...detail.incident, verdict: "degradacao_observada", final: true }];
    return respond(detail, { status: 201 });
  }
  if (url.startsWith("/api/v1/incidents/")) return respond(detail);
  if (url.startsWith("/api/v1/incidents")) return respond(incidents);
  if (init?.method === "POST") return respond({ state: "paused" });
  if (url.startsWith("/api/v1/timeline")) return respond(timeline);
  return respond(status);
}

function respond(body: unknown, init: ResponseInit = { status: 200 }) {
  return Promise.resolve(new Response(JSON.stringify(body), { ...init, headers: { "Content-Type": "application/json" } }));
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  incidents = [];
  vi.useFakeTimers({ shouldAdvanceTime: true });
  fetchMock = vi.fn();
  vi.stubGlobal("fetch", fetchMock);
});

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

test("sem sessão: explica como abrir pelo terminal", async () => {
  fetchMock.mockImplementation(() => respond({ error: "sessão ausente" }, { status: 401 }));
  render(<App />);
  expect(await screen.findByText("pinglume open")).toBeTruthy();
});

test("agente fora do ar sem dados anteriores", async () => {
  fetchMock.mockImplementation(() => Promise.reject(new TypeError("Failed to fetch")));
  render(<App />);
  expect(await screen.findByText("Agente não está respondendo")).toBeTruthy();
  expect(screen.getByText("pinglume run")).toBeTruthy();
});

test("mostra cobertura, lacunas e falhas rotuladas por protocolo", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  expect((await screen.findAllByText("encerramento inesperado do agente")).length).toBeGreaterThan(0);
  expect(screen.getByText("conexão recusada")).toBeTruthy();
  expect(screen.getByText("97%")).toBeTruthy();
  // A barra tem descrição textual equivalente.
  const bar = screen.getByRole("img", { name: /Distribuição do período/ });
  expect(bar.getAttribute("aria-label")).toContain("Agente parado: 2 min");
  expect(bar.getAttribute("aria-label")).toContain("Sem registro: 3 min");
  // Nenhum rótulo afirma perda de pacotes.
  expect(document.body.textContent).not.toMatch(/perda de pacotes(?! IP)/i);
});

test("perde contato depois de ter dados: mantém leitura antiga, avisa e desativa a pausa", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  await screen.findByText("Pausar medição");

  fetchMock.mockImplementation(() => Promise.reject(new TypeError("Failed to fetch")));
  await act(async () => {
    await vi.advanceTimersByTimeAsync(5000);
  });
  expect(screen.getByRole("status").textContent).toContain("Sem contato com o agente");
  expect((screen.getByText("Pausar medição") as HTMLButtonElement).disabled).toBe(true);
  expect(screen.getByText("Estado desconhecido")).toBeTruthy();
});

test("pausar chama a API e atualiza", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  fireEvent.click(await screen.findByText("Pausar medição"));
  await screen.findByText("Medição pausada.");
  expect(fetchMock.mock.calls.some(([u, i]) => u === "/api/v1/pause" && i?.method === "POST")).toBe(true);
});

test("teclado percorre a linha do tempo e lê os valores do trecho", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  const plot = await screen.findByRole("group", { name: "Linha do tempo das sondas" });
  plot.focus();
  fireEvent.keyDown(plot, { key: "Home" });
  // Do início (21:00:00) até o balde de 21:01:00 são 20 trechos de 3 s.
  for (let i = 0; i < 20; i++) fireEvent.keyDown(plot, { key: "ArrowRight" });
  const readout = document.querySelector(".tl-readout")!;
  expect(readout.textContent).toContain("1 execução, 1 ok");
  expect(readout.textContent).toContain("mediana 600 µs");
  fireEvent.keyDown(plot, { key: "ArrowRight" });
  fireEvent.keyDown(plot, { key: "ArrowRight" });
  fireEvent.keyDown(plot, { key: "ArrowRight" });
  expect(readout.textContent).toContain("1 conexão recusada");
});

test("zoom pelo teclado fixa o período e oferece voltar ao vivo", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  const plot = await screen.findByRole("group", { name: "Linha do tempo das sondas" });
  fireEvent.keyDown(plot, { key: "+" });
  expect(await screen.findByText("Voltar ao vivo")).toBeTruthy();
  const last = fetchMock.mock.calls.at(-1)![0] as string;
  expect(last).toMatch(/from=.*&to=/);
  fireEvent.keyDown(plot, { key: "Escape" });
  await act(async () => {});
  expect(screen.queryByText("Voltar ao vivo")).toBeNull();
});

test("relatar: diálogo envia o sintoma e a análise aparece com ressalva de causalidade", async () => {
  fetchMock.mockImplementation(route);
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: "Travou agora" }));
  fireEvent.click(screen.getByLabelText("Vídeo congelou"));
  fireEvent.click(screen.getByText("Registrar relato"));

  const post = await vi.waitFor(() => {
    const c = fetchMock.mock.calls.find(([u, i]) => u === "/api/v1/incidents" && i?.method === "POST");
    if (!c) throw new Error("POST ainda não feito");
    return c;
  });
  expect(JSON.parse(post[1].body)).toMatchObject({ symptom: "video_freeze", note: "" });

  expect(await screen.findByText(/falhou em 5 de 36/)).toBeTruthy();
  expect(screen.getByText(/não prova que ela causou/)).toBeTruthy();
  expect(screen.getAllByText("Degradação observada").length).toBeGreaterThan(0);
  // O relato aparece na linha do tempo como marca própria, distinta das medições.
  expect(screen.getByLabelText(/Relato: Áudio cortou às/)).toBeTruthy();
});

test("apagar relato pede confirmação", async () => {
  incidents = [{ ...detail.incident, verdict: "degradacao_observada", final: true }];
  fetchMock.mockImplementation((url: string, init?: RequestInit) => {
    if (init?.method === "DELETE") return Promise.resolve(new Response(null, { status: 204 }));
    return route(url, init);
  });
  render(<App />);
  fireEvent.click(await screen.findByRole("button", { name: /Áudio cortou/, expanded: false }));
  fireEvent.click(await screen.findByText("Apagar relato"));
  expect(fetchMock.mock.calls.some(([, i]) => i?.method === "DELETE")).toBe(false);
  fireEvent.click(screen.getByText("Apagar"));
  await vi.waitFor(() => {
    if (!fetchMock.mock.calls.some(([u, i]) => u === "/api/v1/incidents/inc1" && i?.method === "DELETE")) {
      throw new Error("DELETE não enviado");
    }
  });
});
