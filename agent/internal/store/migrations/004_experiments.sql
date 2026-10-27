CREATE TABLE experiments (
    id                TEXT PRIMARY KEY,
    device_id         TEXT NOT NULL REFERENCES devices (id),
    question          TEXT NOT NULL,
    intervention      TEXT NOT NULL,
    planned_window_ms INTEGER NOT NULL CHECK (planned_window_ms > 0),
    cancelled_at_ms   INTEGER,
    created_at_ms     INTEGER NOT NULL
);

CREATE TABLE experiment_windows (
    id              TEXT PRIMARY KEY,
    experiment_id   TEXT NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('baseline', 'intervention')),
    start_ms        INTEGER NOT NULL,
    planned_end_ms  INTEGER NOT NULL,
    end_ms          INTEGER,
    UNIQUE (experiment_id, role)
);

CREATE TABLE conditions (
    id         TEXT PRIMARY KEY,
    window_id  TEXT NOT NULL REFERENCES experiment_windows (id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL,
    source     TEXT NOT NULL CHECK (source IN ('declared', 'detected')),
    method     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE comparison_results (
    experiment_id  TEXT NOT NULL REFERENCES experiments (id) ON DELETE CASCADE,
    rule_version   TEXT NOT NULL,
    verdict        TEXT NOT NULL,
    body_json      TEXT NOT NULL,
    created_at_ms  INTEGER NOT NULL,
    PRIMARY KEY (experiment_id, rule_version)
);
