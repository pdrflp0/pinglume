import type { Timeline as TimelineData } from "./api";
import { clock, duration, failureLabel, latency, stateLabel } from "./format";
import { coverageSegments } from "./Timeline";

const fmt = new Intl.DateTimeFormat("pt-BR", { hour: "2-digit", minute: "2-digit", second: "2-digit" });

/** Equivalente textual do gráfico: os mesmos números, em tabela. */
export function DataTable({ data }: { data: TimelineData }) {
  const from = Date.parse(data.from);
  const to = Date.parse(data.to);
  const segs = coverageSegments(data.coverage, from, to);
  return (
    <details className="datatable">
      <summary>Ver os dados do gráfico como tabela</summary>

      <h3>Cobertura</h3>
      <div className="table-wrap">
        <table>
          <caption className="sr-only">Períodos de cobertura no intervalo exibido</caption>
          <thead>
            <tr>
              <th scope="col">Início</th>
              <th scope="col">Fim</th>
              <th scope="col">Duração</th>
              <th scope="col">Estado</th>
              <th scope="col">Motivo</th>
            </tr>
          </thead>
          <tbody>
            {segs.map((s) => (
              <tr key={`${s.start}-${s.state}`}>
                <td className="num">{fmt.format(s.start)}</td>
                <td className="num">{fmt.format(s.end)}</td>
                <td className="num">{duration(s.end - s.start)}</td>
                <td>{stateLabel[s.state]}</td>
                <td className="muted">{s.reason || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {data.series.map((s) => (
        <div key={s.probeDefId}>
          <h3>
            {s.endpoint} · {s.kind.toUpperCase()} v{s.protocolVersion} · {s.target}
          </h3>
          <div className="table-wrap">
            <table>
              <caption className="sr-only">
                Trechos de {duration(data.bucketMs)} com execuções de {s.endpoint}
              </caption>
              <thead>
                <tr>
                  <th scope="col">Trecho</th>
                  <th scope="col" className="r">Execuções</th>
                  <th scope="col" className="r">OK</th>
                  <th scope="col">Falhas</th>
                  <th scope="col" className="r">Mediana</th>
                  <th scope="col" className="r">P95</th>
                </tr>
              </thead>
              <tbody>
                {s.buckets.map((b) => (
                  <tr key={b.t}>
                    <td className="num">
                      {fmt.format(b.t)} – {fmt.format(b.t + data.bucketMs)}
                    </td>
                    <td className="r num">{b.n}</td>
                    <td className="r num">{b.ok}</td>
                    <td>
                      {b.fail
                        ? Object.entries(b.fail)
                            .map(([k, v]) => `${v} ${failureLabel[k] ?? k}`)
                            .join(", ")
                        : "—"}
                    </td>
                    <td className="r num">{latency(b.p50Us)}</td>
                    <td className="r num">{latency(b.p95Us)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ))}
      <p className="muted small">
        Trechos sem execução não aparecem. Dados de {clock(data.from)} a {clock(data.to)}.
      </p>
    </details>
  );
}
