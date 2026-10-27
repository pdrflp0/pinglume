CREATE TABLE incidents (
    id                TEXT PRIMARY KEY,
    device_id         TEXT NOT NULL REFERENCES devices (id),
    reported_at_ms    INTEGER NOT NULL,
    created_at_ms     INTEGER NOT NULL,
    symptom           TEXT NOT NULL CHECK (symptom IN ('audio_cut', 'video_freeze', 'dropped_call', 'slow', 'other')),
    note              TEXT NOT NULL DEFAULT '',
    created_via       TEXT NOT NULL CHECK (created_via IN ('dashboard', 'cli'))
);
CREATE INDEX incidents_reported ON incidents (reported_at_ms);

CREATE TABLE interpretations (
    id             TEXT PRIMARY KEY,
    incident_id    TEXT NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    rule_version   TEXT NOT NULL,
    verdict        TEXT NOT NULL,
    body_json      TEXT NOT NULL,
    created_at_ms  INTEGER NOT NULL,
    UNIQUE (incident_id, rule_version)
);
