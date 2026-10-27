CREATE TABLE devices (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    os            TEXT NOT NULL,
    arch          TEXT NOT NULL,
    agent_version TEXT NOT NULL,
    created_at_ms INTEGER NOT NULL
);

CREATE TABLE endpoints (
    id     TEXT PRIMARY KEY,
    label  TEXT NOT NULL,
    host   TEXT NOT NULL,
    region TEXT NOT NULL DEFAULT '',
    owner  TEXT NOT NULL,
    UNIQUE (label, host, owner)
);

CREATE TABLE probe_definitions (
    id               TEXT PRIMARY KEY,
    kind             TEXT NOT NULL CHECK (kind IN ('dns', 'tcp', 'https', 'udp_echo')),
    endpoint_id      TEXT NOT NULL REFERENCES endpoints (id),
    target           TEXT NOT NULL,
    interval_ms      INTEGER NOT NULL CHECK (interval_ms > 0),
    timeout_ms       INTEGER NOT NULL CHECK (timeout_ms > 0),
    protocol_version INTEGER NOT NULL,
    params_json      TEXT NOT NULL DEFAULT '{}',
    created_at_ms    INTEGER NOT NULL,
    UNIQUE (kind, endpoint_id, target, interval_ms, timeout_ms, protocol_version, params_json)
);

CREATE TABLE observations (
    id              TEXT PRIMARY KEY,
    device_id       TEXT NOT NULL REFERENCES devices (id),
    probe_def_id    TEXT NOT NULL REFERENCES probe_definitions (id),
    seq             INTEGER NOT NULL,
    started_wall_ms INTEGER NOT NULL,
    duration_us     INTEGER NOT NULL,
    status          TEXT NOT NULL CHECK (status IN (
                        'ok', 'timeout', 'refused', 'dns_error', 'tls_error', 'http_error',
                        'net_unreachable', 'cancelled', 'error')),
    error_detail    TEXT NOT NULL DEFAULT '',
    phases_json     TEXT NOT NULL DEFAULT '{}',
    received_at_ms  INTEGER NOT NULL,
    UNIQUE (probe_def_id, seq)
);
CREATE INDEX observations_started ON observations (started_wall_ms);

CREATE TABLE system_events (
    id        TEXT PRIMARY KEY,
    at_wall_ms INTEGER NOT NULL,
    kind      TEXT NOT NULL,
    detail    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX system_events_at ON system_events (at_wall_ms);
