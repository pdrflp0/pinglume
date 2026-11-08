import { useCallback, useEffect, useRef, useState, type CSSProperties, type ReactNode } from "react";
import {
  api,
  ApiError,
  type CoverageState,
  type FailureKind,
  type IncidentListItem,
  type Status,
  type StorageInfo,
  type Timeline as TimelineData,
} from "./api";
import { Mark } from "./Brand";
import { ComparisonCard } from "./Comparison";
import { DataTable } from "./DataTable";
import { ExportCard } from "./Export";
import { Hero } from "./Hero";
import { IncidentsCard, ReportDialog } from "./Incidents";
import {
  clock,
  duration,
  failureLabel,
  kindMeasure,
  kindName,
  latency,
  megabytes,
  percent,
  stateLabel,
  stateOrder,
  udpLoss,
  until,
} from "./format";
import { pan, PRESETS, resolve, select, toQuery, zoom, type ViewRange } from "./range";
import { Rail } from "./Rail";
import { rangeLabel } from "./scale";
import { Timeline } from "./Timeline";

const POLL_MS = 5000;
const BUCKETS = 300;

interface Load {
  status: Status | null;
  timeline: TimelineData | null;
  incidents: IncidentListItem[];
  failure: { kind: FailureKind; message: string; since: Date } | null;
  loading: boolean;
}

function useDashboard(range: ViewRange) {
  const [load, setLoad] = useState<Load>({
    status: null,
    timeline: null,
    incidents: [],
    failure: null,
    loading: true,
  });

  const refresh = useCallback(async () => {
    const q = toQuery(range);
    try {
      const [status, timeline, incidents] = await Promise.all([
        api.status(q),
        api.timeline(q, BUCKETS),
        api.incidents(q),
      ]);
      setLoad({ status, timeline, incidents, failure: null, loading: false });
    } catch (e) {
      const err = e instanceof ApiError ? e : new ApiError("error", String(e));

      setLoad((prev) => ({
        ...prev,
        failure: { kind: err.kind, message: err.message, since: prev.failure?.since ?? new Date() },
        loading: false,
      }));
    }
  }, [range]);

  useEffect(() => {
    refresh();
    const id = setInterval(refresh, POLL_MS);
    return () => clearInterval(id);
  }, [refresh]);

  return { ...load, refresh };
}

export function App() {
  const [range, setRange] = useState<ViewRange>({ live: true, spanMs: PRESETS[0].spanMs });
  const { status: data, timeline, incidents, failure, loading, refresh } = useDashboard(range);
  const [busy, setBusy] = useState(false);
  const [reporting, setReporting] = useState(false);
  const [selectedIncident, setSelectedIncident] = useState<string | null>(null);
  const [storage, setStorage] = useState<StorageInfo | null>(null);
  useEffect(() => {
    const load = () => api.storage().then(setStorage).catch(() => {});
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  }, []);
  const [announcement, setAnnouncement] = useState("");
  const [actionError, setActionError] = useState<string | null>(null);

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
  const now = Date.parse(data.now);
  const shown = resolve(range, now);

  return (
    <div className="shell">
      <Rail measuring={!offline && data.state === "measuring"} />
      <div className="stage">
        <header className="topbar">
          <div>
            <h1 className="sr-only">Pinglume</h1>
            <p className="eyebrow">Monitor local de conexão</p>
          </div>
          <div className="top-actions">
            <button className="btn btn-report" onClick={() => setReporting(true)} disabled={offline}>
              Travou agora
            </button>
            <button className="btn-ghost" onClick={toggle} disabled={busy || offline}>
              {busy ? "Aplicando…" : paused ? "Retomar medição" : "Pausar medição"}
            </button>
          </div>
        </header>

        <div className="sr-only" aria-live="polite">
          {announcement}
        </div>

        <ReportDialog
          open={reporting}
          onClose={() => setReporting(false)}
          onCreated={(d) => {
            setReporting(false);
            setSelectedIncident(d.incident.id);
            setAnnouncement(
              `Relato registrado às ${clock(d.incident.reportedAt)}. A análise fica pronta às ${clock(d.analysis.windowTo)}.`,
            );
            refresh();
          }}
        />

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

        <main className="grid">
          <div className="slot s-visao" id="visao">
            <Hero status={data} reports={incidents.length} offline={offline} />
          </div>

          <div className="slot s-tempo" id="tempo">
            <RangeBar
              range={range}
              label={rangeLabel(shown.from, shown.to)}
              onChange={setRange}
              onZoom={(f) => setRange(zoom(range, now, f))}
              onPan={(f) => setRange(pan(range, now, f))}
            />
            <section aria-labelledby="tl-title" className="card reveal" style={{ "--i": 3 } as CSSProperties}>
              <h2 id="tl-title">Linha do tempo</h2>
              {timeline && (
                <>
                  <Timeline
                    data={timeline}
                    incidents={incidents}
                    onIncident={setSelectedIncident}
                    onSelect={(a, b) => setRange(select(a, b, now))}
                    onZoom={(f, c) => setRange(zoom(range, now, f, c))}
                    onPan={(f) => setRange(pan(range, now, f))}
                    onLive={() => setRange({ live: true, spanMs: PRESETS[0].spanMs })}
                  />
                  <DataTable data={timeline} />
                </>
              )}
            </section>
          </div>

          <div className="slot s-relatos" id="relatos">
            <IncidentsCard
              items={incidents}
              selected={selectedIncident}
              onSelect={setSelectedIncident}
              onDeleted={() => {
                setSelectedIncident(null);
                setAnnouncement("Relato apagado.");
                refresh();
              }}
            />
          </div>

          <div className="slot s-comparacao" id="comparacao">
            <ComparisonCard />
          </div>

          <div className="slot s-cobertura" id="cobertura">
            <section aria-labelledby="cov-title" className="card">
              <h2 id="cov-title">Cobertura</h2>
              <p className="muted">
                Tempo em que o agente estava (ou não) medindo no período exibido. Período sem medição não indica
                conexão boa nem queda.
              </p>
              <CoverageBar stateMs={data.summary.stateMs} />
              <GapList gaps={data.summary.gaps} />
            </section>
          </div>

          <div className="slot s-exportar" id="exportar">
            <ExportCard from={shown.from} to={shown.to} />
          </div>

          <div className="slot s-sondas" id="sondas">
            <section aria-labelledby="probes-title" className="card">
              <h2 id="probes-title">Sondas</h2>
              <ProbeTable probes={data.summary.probes} />
            </section>
          </div>
        </main>

        <footer className="foot muted">
          Agente {data.agentVersion} · atualiza a cada {POLL_MS / 1000} s · dados somente neste computador
          {storage && (
            <>
              {" "}
              · banco {megabytes(storage.usage.usedBytes)}
              {storage.limitBytes > 0 && <> de {megabytes(storage.limitBytes)}</>} · medições brutas guardadas por{" "}
              {duration(storage.rawRetentionMs)}, agregados por {duration(storage.aggregateRetentionMs)}
            </>
          )}
        </footer>
      </div>
    </div>
  );
}

interface RangeBarProps {
  range: ViewRange;
  label: string;
  onChange: (r: ViewRange) => void;
  onZoom: (factor: number) => void;
  onPan: (fraction: number) => void;
}

function RangeBar({ range, label, onChange, onZoom, onPan }: RangeBarProps) {
  return (
    <div className="rangebar" role="toolbar" aria-label="Período exibido">
      <fieldset className="segmented">
        <legend className="sr-only">Últimos</legend>
        {PRESETS.map((p) => {
          const on = range.live && range.spanMs === p.spanMs;
          return (
            <label key={p.window} className={on ? "on" : undefined}>
              <input
                type="radio"
                name="preset"
                checked={on}
                onChange={() => onChange({ live: true, spanMs: p.spanMs })}
              />
              {p.label}
            </label>
          );
        })}
      </fieldset>
      <div className="rangebar-nav">
        <button className="btn-ghost" onClick={() => onPan(-0.5)} aria-label="Deslocar para o passado">
          ‹
        </button>
        <button className="btn-ghost" onClick={() => onZoom(2)} aria-label="Afastar">
          −
        </button>
        <button className="btn-ghost" onClick={() => onZoom(0.5)} aria-label="Aproximar">
          +
        </button>
        <button
          className="btn-ghost"
          onClick={() => onPan(0.5)}
          disabled={range.live}
          aria-label="Deslocar para o presente"
        >
          ›
        </button>
      </div>
      <span className="rangebar-label num">
        {label}
        {range.live ? <span className="live"> · ao vivo</span> : null}
      </span>
      {!range.live && (
        <button className="btn-ghost" onClick={() => onChange({ live: true, spanMs: PRESETS[0].spanMs })}>
          Voltar ao vivo
        </button>
      )}
    </div>
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
              {clock(g.start)} – {until(g.start, g.end)}
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
                    {kindName(p.kind)} v{p.protocolVersion}
                    <div className="muted small">a cada {duration(p.intervalMs)}</div>
                    <div className="muted small">{kindMeasure[p.kind] ?? p.kind}</div>
                  </td>
                  <td className="r num">{p.total}</td>
                  <td className="r num">
                    {percent(p.coverage)}
                    {p.backoffMs > 0 && (
                      <div className="muted small">{duration(p.backoffMs)} em backoff</div>
                    )}
                  </td>
                  <td className="r num">{p.ok}</td>
                  <td>
                    {p.udpLossRate !== undefined && p.udpSent ? (
                      <>
                        {udpLoss(p.udpLost ?? 0, p.udpSent)}
                        <div className="muted small">jitter {latency(p.jitterP50Us ?? null)}</div>
                      </>
                    ) : fails.length === 0 ? (
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
                  <td className="r num">
                    {latency(p.p50Us)}
                    {p.approximate && <div className="muted small">aprox.</div>}
                  </td>
                  <td className="r num">
                    {latency(p.p95Us)}
                    {p.approximate && <div className="muted small">aprox.</div>}
                  </td>
                  <td className="r num">{clock(p.lastStarted)}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="muted small">
        Cobertura: execuções concluídas ÷ esperadas enquanto o agente media. Backoff: depois de falhas seguidas, o
        agente amplia o intervalo para não insistir no endpoint, e a cobertura cai de propósito. P50/P95: só execuções
        bem-sucedidas; "aprox." quando o período inclui dados antigos já compactados em agregados por minuto (as contagens
        continuam exatas). Falhas são rotuladas pelo protocolo da sonda; não são perda de pacotes IP. Na sonda UDP, a perda é de datagramas
        da própria sonda.
      </p>
    </>
  );
}

function Centered({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <main className="centered">
      <div className="card narrow">
        <div className="brand">
          <Mark size={34} />
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
