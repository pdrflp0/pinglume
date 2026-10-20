-- Fonte da verdade para "havia medição neste instante?".
-- Um intervalo aberto (is_open = 1) tem end_wall_ms atualizado a cada heartbeat:
-- se o agente morrer, o intervalo termina no último instante em que estava vivo.
CREATE TABLE coverage_intervals (
    id            TEXT PRIMARY KEY,
    device_id     TEXT NOT NULL REFERENCES devices (id),
    start_wall_ms INTEGER NOT NULL,
    end_wall_ms   INTEGER NOT NULL,
    state         TEXT NOT NULL CHECK (state IN (
                      'measuring', 'paused', 'suspended', 'agent_down', 'probe_unavailable', 'shutting_down')),
    reason        TEXT NOT NULL DEFAULT '',
    detected_by   TEXT NOT NULL DEFAULT '',
    is_open       INTEGER NOT NULL DEFAULT 0 CHECK (is_open IN (0, 1))
);
CREATE INDEX coverage_intervals_range ON coverage_intervals (start_wall_ms, end_wall_ms);
-- No máximo um intervalo aberto por dispositivo.
CREATE UNIQUE INDEX coverage_intervals_one_open ON coverage_intervals (device_id) WHERE is_open = 1;

CREATE TABLE agent_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
