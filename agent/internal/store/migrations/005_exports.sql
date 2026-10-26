-- Snapshot de exportação: gerado uma vez e nunca alterado. O hash permite
-- conferir que um arquivo compartilhado é exatamente o que foi gerado.
CREATE TABLE export_snapshots (
    id              TEXT PRIMARY KEY,
    created_at_ms   INTEGER NOT NULL,
    from_ms         INTEGER NOT NULL,
    to_ms           INTEGER NOT NULL,
    options_json    TEXT NOT NULL,
    schema_version  TEXT NOT NULL,
    agent_version   TEXT NOT NULL,
    sha256          TEXT NOT NULL,   -- do corpo JSON
    body_json       TEXT NOT NULL,
    body_html       TEXT NOT NULL
);
CREATE INDEX export_snapshots_created ON export_snapshots (created_at_ms);
