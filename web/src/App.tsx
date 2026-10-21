import { useCallback, useEffect, useRef, useState, type ReactNode } from "react";
import { api, ApiError, type CoverageState, type FailureKind, type Status } from "./api";
import { clock, duration, failureLabel, latency, percent, stateLabel, stateOrder } from "./format";

const WINDOWS = [
  { value: "15m", label: "15 min" },
  { value: "1h", label: "1 h" },
  { value: "6h", label: "6 h" },
  { value: "24h", label: "24 h" },
] as const;

const POLL_MS = 5000;

interface Load {
  data: Status | null;
  failure: { kind: FailureKind; message: string; since: Date } | null;
  loading: boolean;
}

function useStatus(period: string) {
  const [load, setLoad] = useState<Load>({ data: null, failure: null, loading: true });

  const refresh = useCallback(async () => {
    try {
      const data = await api.status(period);
      setLoad({ data, failure: null, loading: false });
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError("error", String(e));
      // Mantém a última leitura boa e o instante da primeira falha.
      setLoad((prev) => ({
        data: prev.data,
        failure: { kind: err.kind, message: err.message, since: prev.failure?.since ?? new Date() },
        loading: false,
      }));
    }
  }, [period]);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, POLL_MS);
    return () => clearInterval(id);
  }, [refresh]);

  return { ...load, refresh };
}

export function App() {
  const [period, setPeriod] = useState<string>("15m");
  const { data, failure, loading, refresh } = useStatus(period);
  const [busy, setBusy] = useState(false);
  const [announcement, setAnnouncement] = useState("");
  const [actionError, setActionError] = useState<string | null>(null);

  // Anuncia mudanças de estado do agente para leitores de tela.
  const lastState = useRef<CoverageState | null>(null);
  useEffect(() => {
    if (data && lastState.current && lastState.current !== data.state) {
      setAnnouncement(`Agente agora: ${stateLabel[data.state]}.`);
    }
    lastState.current = data?.state ?? null;
  }, [data]);

  async function toggle() {
    if (!data) return;
    setBusy(true);
    setActionError(null);
    try {
      const res = data.state === "paused" ? await api.resume() : await api.pause();
      setAnnouncement(res.state === "paused" ? "Medição pausada." : "Medição retomada.");
      await refresh();
    } catch (e) {
      setActionError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  if (failure?.kind === "unauthenticated") return <NeedsAuth />;
  if (!data && loading) return <Centered title="Conectando ao agente…" />;
  if (!data && failure) return <AgentUnreachable failure={failure} />;
  if (!data) return null;

  const offline = failure !== null;
  const paused = data.state === "paused";

  return (
    <div className="page">
      <header className="top">
        <div className="brand">
          <span className="logo" aria-hidden="true" />
          <h1>Pinglume</h1>
        </div>
        <div className="top-actions">
          <StateChip state={offline ? null : data.state} />
          <button className="btn" onClick={toggle} disabled={busy || offline}>
            {busy ? "Aplicando…" : paused ? "Retomar medição" : "Pausar medição"}
          </button>
        </div>
      </header>

      <div className="sr-only" aria-live="polite">
        {announcement}
      </div>

      {offline && (
        <div className="banner banner-warn" role="status">
          <strong>Sem contato com o agente desde {clock(failure.since.toISOString())}.</strong> Os números abaixo são
          da última leitura ({clock(data.now)}) e não estão sendo atualizados. {failure.message}
        </div>
      )}
      {actionError && (
        <div className="banner banner-error" role="alert">
          Não foi possível mudar o estado: {actionError}
        </div>
      )}

      <main>
        <section aria-labelledby="cov-title" className="card">
          <div className="card-head">
            <h2 id="cov-title">Cobertura</h2>
            <WindowPicker value={period} onChange={setPeriod} />
          </div>
          <p className="muted">
            {clock(data.summary.from)} – {clock(data.summary.to)}. Tempo em que o agente estava (ou não) medindo.
            Período sem medição não indica conexão boa nem queda.
          </p>
          <CoverageBar stateMs={data.summary.stateMs} />
          <GapList gaps={data.summary.gaps} />
        </section>

        <section aria-labelledby="probes-title" className="card">
          <h2 id="probes-title">Sondas</h2>
          <ProbeTable probes={data.summary.probes} />
        </section>
      </main>

      <footer className="foot muted">
        Agente {data.agentVersion} · atualiza a cada {POLL_MS / 1000} s · dados somente neste computador
      </footer>
    </div>
  );
}

function StateChip({ state }: { state: CoverageState | null }) {
  if (state === null) {
    return <span className="chip chip-unknown">Estado desconhecido</span>;
  }
  return (
    <span className={`chip chip-${state}`}>
      <span className="chip-dot" aria-hidden="true" />
      {stateLabel[state]}
    </span>
  );
}

function WindowPicker({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <fieldset className="segmented">
      <legend className="sr-only">Período</legend>
      {WINDOWS.map((w) => (
        <label key={w.value} className={value === w.value ? "on" : undefined}>
          <input
            type="radio"
            name="window"
            value={w.value}
            checked={value === w.value}
            onChange={() => onChange(w.value)}
          />
          {w.label}
        </label>
      ))}
    </fieldset>
  );
}

function CoverageBar({ stateMs }: { stateMs: Status["summary"]["stateMs"] }) {
  const entries = stateOrder.map((s) => [s, stateMs[s] ?? 0] as const).filter(([, ms]) => ms > 0);
  const total = entries.reduce((acc, [, ms]) => acc + ms, 0);
  if (total === 0) return null;
  const description = entries.map(([s, ms]) => `${stateLabel[s]}: ${duration(ms)}`).join("; ");
  return (
    <>
      <div className="covbar" role="img" aria-label={`Distribuição do período. ${description}.`}>
        {entries.map(([s, ms]) => (
          <span key={s} className={`seg seg-${s}`} style={{ flexGrow: ms }} />
        ))}
      </div>
      <ul className="legend">
        {entries.map(([s, ms]) => (
          <li key={s}>
            <span className={`swatch seg-${s}`} aria-hidden="true" />
            <span>{stateLabel[s]}</span>
            <span className="num">{duration(ms)}</span>
            <span className="muted num">({Math.round((ms / total) * 100)}%)</span>
          </li>
        ))}
      </ul>
    </>
  );
}

function GapList({ gaps }: { gaps: Status["summary"]["gaps"] }) {
  if (gaps.length === 0) {
    return <p className="muted small">Nenhum período sem medição registrado nesta janela.</p>;
  }
  return (
    <div>
      <h3>Períodos sem medição</h3>
      <ol className="gaps">
        {gaps.map((g) => (
          <li key={`${g.start}-${g.state}`}>
            <span className="num">
              {clock(g.start)} – {clock(g.end)}
            </span>
            <span className={`tag tag-${g.state}`}>{stateLabel[g.state]}</span>
            <span className="muted">{g.reason}</span>
          </li>
        ))}
      </ol>
    </div>
  );
}

function ProbeTable({ probes }: { probes: Status["summary"]["probes"] }) {
  if (probes.length === 0) {
    return (
      <p className="muted">
        Nenhuma observação nesta janela. Isso significa sem cobertura, não que a conexão esteve boa.
      </p>
    );
  }
  return (
    <>
      <div className="table-wrap">
        <table>
          <caption className="sr-only">Resumo por sonda no período selecionado</caption>
          <thead>
            <tr>
              <th scope="col">Endpoint</th>
              <th scope="col">Sonda</th>
              <th scope="col" className="r">Execuções</th>
              <th scope="col" className="r">Cobertura</th>
              <th scope="col" className="r">OK</th>
              <th scope="col">Falhas</th>
              <th scope="col" className="r">P50</th>
              <th scope="col" className="r">P95</th>
              <th scope="col" className="r">Última</th>
            </tr>
          </thead>
          <tbody>
            {probes.map((p) => {
              const fails = Object.entries(p.failures).sort();
              return (
                <tr key={`${p.endpoint}-${p.kind}-${p.target}-${p.protocolVersion}`}>
                  <th scope="row">
                    {p.endpoint}
                    <div className="muted small mono">{p.target}</div>
                  </th>
                  <td>
                    {p.kind.toUpperCase()} v{p.protocolVersion}
                    <div className="muted small">a cada {duration(p.intervalMs)}</div>
                  </td>
                  <td className="r num">{p.total}</td>
                  <td className="r num">{percent(p.coverage)}</td>
                  <td className="r num">{p.ok}</td>
                  <td>
                    {fails.length === 0 ? (
                      <span className="muted">nenhuma</span>
                    ) : (
                      <ul className="fails">
                        {fails.map(([k, v]) => (
                          <li key={k}>
                            <span className="num">{v}</span> {failureLabel[k] ?? k}
                          </li>
                        ))}
                      </ul>
                    )}
                  </td>
                  <td className="r num">{latency(p.p50Us)}</td>
                  <td className="r num">{latency(p.p95Us)}</td>
                  <td className="r num">{clock(p.lastStarted)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="muted small">
        Cobertura: execuções concluídas ÷ esperadas enquanto o agente media. P50/P95: tempo de handshake só das
        execuções bem-sucedidas. Falhas são rotuladas pelo protocolo da sonda; não são perda de pacotes IP.
      </p>
    </>
  );
}

function Centered({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <main className="centered">
      <div className="card narrow">
        <div className="brand">
          <span className="logo" aria-hidden="true" />
          <h1>{title}</h1>
        </div>
        {children}
      </div>
    </main>
  );
}

function NeedsAuth() {
  return (
    <Centered title="Abra pelo terminal">
      <p>
        Por segurança, o dashboard só abre com um link de uso único gerado no seu computador. No terminal, rode:
      </p>
      <pre className="cmd">pinglume open</pre>
      <p className="muted small">O link vale por 60 segundos e cria uma sessão neste navegador.</p>
    </Centered>
  );
}

function AgentUnreachable({ failure }: { failure: NonNullable<Load["failure"]> }) {
  return (
    <Centered title="Agente não está respondendo">
      <p>{failure.message}</p>
      <p>Para iniciar a coleta, rode no terminal:</p>
      <pre className="cmd">pinglume run</pre>
      <p className="muted small">Esta página tenta de novo a cada {POLL_MS / 1000} segundos.</p>
    </Centered>
  );
}
