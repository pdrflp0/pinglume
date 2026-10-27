CREATE TABLE observation_minutes (
    probe_def_id  TEXT NOT NULL REFERENCES probe_definitions (id),
    device_id     TEXT NOT NULL REFERENCES devices (id),
    minute_ms     INTEGER NOT NULL,
    n             INTEGER NOT NULL,
    ok            INTEGER NOT NULL,
    failures_json TEXT NOT NULL DEFAULT '{}',
    p50_us        INTEGER,
    p95_us        INTEGER,
    min_us        INTEGER,
    max_us        INTEGER,
    PRIMARY KEY (probe_def_id, minute_ms)
);
CREATE INDEX observation_minutes_minute ON observation_minutes (minute_ms);
