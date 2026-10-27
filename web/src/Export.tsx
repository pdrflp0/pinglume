import { useEffect, useState } from "react";
import { api, type ExportMeta } from "./api";
import { clock } from "./format";
import { rangeLabel } from "./scale";

export function ExportCard({ from, to }: { from: number; to: number }) {
  const [incidents, setIncidents] = useState(true);
  const [comparisons, setComparisons] = useState(true);
  const [notes, setNotes] = useState(false);
  const [preview, setPreview] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [list, setList] = useState<ExportMeta[]>([]);
  const [created, setCreated] = useState<ExportMeta | null>(null);

  const load = () => api.exports().then(setList).catch(() => {});
  useEffect(() => {
    load();
  }, []);

  const params = new URLSearchParams({
    from: new Date(from).toISOString(),
    to: new Date(to).toISOString(),
    incidents: incidents ? "1" : "0",
    comparisons: comparisons ? "1" : "0",
    notes: incidents && notes ? "1" : "0",
  });

  async function generate() {
    setBusy(true);
    setError(null);
    try {
      const m = await api.createExport({
        from: new Date(from).toISOString(),
        to: new Date(to).toISOString(),
        includeIncidents: incidents,
        includeComparisons: comparisons,
        includeNotes: incidents && notes,
      });
      setCreated(m);
      setPreview(null);
      load();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby="exp-title" className="card">
      <h2 id="exp-title">Exportar relatório</h2>
      <p className="muted">
        Gera um HTML legível e um JSON com os mesmos dados do período exibido ({rangeLabel(from, to)}). Nome do
        computador, endereços, nomes de interface e detalhes técnicos de erro são removidos; o relatório lista o que foi
        removido.
      </p>
      <fieldset className="choices one-col">
        <legend>Incluir</legend>
        <label>
          <input type="checkbox" checked disabled /> Cobertura e resumo das sondas
        </label>
        <label>
          <input type="checkbox" checked={incidents} onChange={(e) => setIncidents(e.target.checked)} /> Relatos e
          análises
        </label>
        <label className={incidents ? "" : "muted"}>
          <input type="checkbox" checked={notes && incidents} disabled={!incidents} onChange={(e) => setNotes(e.target.checked)} />{" "}
          Observações escritas nos relatos (com e-mails, números e endereços removidos)
        </label>
        <label>
          <input type="checkbox" checked={comparisons} onChange={(e) => setComparisons(e.target.checked)} /> Comparações
          guiadas concluídas
        </label>
      </fieldset>
      <div className="dialog-actions start">
        <button className="btn-ghost" onClick={() => setPreview(`/api/v1/exports/preview?${params}`)}>
          {preview ? "Atualizar prévia" : "Ver prévia"}
        </button>
        <button className="btn" onClick={generate} disabled={busy}>
          {busy ? "Gerando…" : "Gerar arquivos"}
        </button>
      </div>
      {preview && (
        <p className="small">
          <a href={preview} target="_blank" rel="noopener noreferrer">
            Abrir prévia em nova aba
          </a>{" "}
          <span className="muted">(se a prévia abaixo não aparecer, o navegador pode estar bloqueando quadros)</span>
        </p>
      )}
      {preview && (
        <iframe
          className="export-preview"
          title="Prévia do relatório exportado"
          src={preview}
          sandbox=""
          referrerPolicy="no-referrer"
        />
      )}
      {error && (
        <p className="banner banner-error" role="alert">
          {error}
        </p>
      )}
      {created && (
        <div className="banner banner-warn" role="status">
          <strong>Arquivos gerados.</strong> Depois de baixado e enviado, um arquivo não pode ser revogado: quem o
          receber pode guardá-lo e repassá-lo.
          <DownloadLinks m={created} />
        </div>
      )}
      {list.length > 0 && (
        <details className="past">
          <summary>Exportações anteriores ({list.length})</summary>
          <ul className="exports">
            {list.map((m) => (
              <li key={m.id}>
                <span className="num">{clock(m.createdAt)}</span> · {rangeLabel(Date.parse(m.from), Date.parse(m.to))}
                <DownloadLinks m={m} />
              </li>
            ))}
          </ul>
        </details>
      )}
    </section>
  );
}

function DownloadLinks({ m }: { m: ExportMeta }) {
  return (
    <div className="downloads">
      <a href={`/api/v1/exports/${m.id}/download?format=html`} download>
        Baixar HTML
      </a>
      <a href={`/api/v1/exports/${m.id}/download?format=json`} download>
        Baixar JSON
      </a>
      <span className="muted small mono" title="SHA-256 do JSON">
        SHA-256 {m.sha256.slice(0, 16)}…
      </span>
    </div>
  );
}
