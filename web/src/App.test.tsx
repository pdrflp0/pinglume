import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
import { App } from "./App";
import type { Status } from "./api";

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
        lastStarted: "2026-10-20T21:14:55Z",
      },
    ],
  },
};

function respond(body: unknown, init: ResponseInit = { status: 200 }) {
  return Promise.resolve(new Response(JSON.stringify(body), { ...init, headers: { "Content-Type": "application/json" } }));
}

let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
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
  fetchMock.mockImplementation(() => respond(status));
  render(<App />);
  expect(await screen.findByText("encerramento inesperado do agente")).toBeTruthy();
  expect(screen.getByText("conexão recusada")).toBeTruthy();
  expect(screen.getByText("97%")).toBeTruthy();
  // A barra tem descrição textual equivalente.
  const bar = screen.getByRole("img");
  expect(bar.getAttribute("aria-label")).toContain("Agente parado: 2 min");
  expect(bar.getAttribute("aria-label")).toContain("Sem registro: 3 min");
  // Nenhum rótulo afirma perda de pacotes.
  expect(document.body.textContent).not.toMatch(/perda de pacotes(?! IP)/i);
});

test("perde contato depois de ter dados: mantém leitura antiga, avisa e desativa a pausa", async () => {
  fetchMock.mockImplementationOnce(() => respond(status));
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
  fetchMock.mockImplementation((_url: string, init?: RequestInit) => {
    if (init?.method === "POST") return respond({ state: "paused" });
    return respond(status);
  });
  render(<App />);
  fireEvent.click(await screen.findByText("Pausar medição"));
  await screen.findByText("Medição pausada.");
  expect(fetchMock.mock.calls.some(([u, i]) => u === "/api/v1/pause" && i?.method === "POST")).toBe(true);
});
