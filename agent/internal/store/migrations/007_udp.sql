ALTER TABLE observation_minutes ADD COLUMN udp_sent INTEGER NOT NULL DEFAULT 0;
ALTER TABLE observation_minutes ADD COLUMN udp_lost INTEGER NOT NULL DEFAULT 0;
ALTER TABLE observation_minutes ADD COLUMN jitter_p50_us INTEGER;
