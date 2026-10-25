-- Relatos de quem usa ("travou agora"). São relatos, não medições: nada aqui
-- é derivado automaticamente das sondas.
CREATE TABLE incidents (
    id                TEXT PRIMARY KEY,
    device_id         TEXT NOT NULL REFERENCES devices (id),
    reported_at_ms    INTEGER NOT NULL,  -- quando a pessoa diz que aconteceu
    created_at_ms     INTEGER NOT NULL,  -- quando o relato foi gravado
    symptom           TEXT NOT NULL CHECK (symptom IN ('audio_cut', 'video_freeze', 'dropped_call', 'slow', 'other')),
    note              TEXT NOT NULL DEFAULT '',
    created_via       TEXT NOT NULL CHECK (created_via IN ('dashboard', 'cli'))
);
CREATE INDEX incidents_reported ON incidents (reported_at_ms);

-- Interpretação gravada quando a janela de análise já fechou: fica imutável e
-- ligada à versão da regra que a produziu.
CREATE TABLE interpretations (
    id             TEXT PRIMARY KEY,
    incident_id    TEXT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    rule_version   TEXT NOT NULL,
    verdict        TEXT NOT NULL,
    body_json      TEXT NOT NULL,
    created_at_ms  INTEGER NOT NULL,
    UNIQUE (incident_id, rule_version)
);
