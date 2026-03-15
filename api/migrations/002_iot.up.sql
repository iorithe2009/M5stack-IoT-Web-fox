-- デバイスマスタ
CREATE TABLE IF NOT EXISTS devices (
  id         BIGSERIAL PRIMARY KEY,
  device_key TEXT NOT NULL UNIQUE,
  name       TEXT NOT NULL DEFAULT '',
  type       TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- デバイス最新状態（1デバイス1行）
CREATE TABLE IF NOT EXISTS device_state (
  device_id    BIGINT PRIMARY KEY REFERENCES devices(id),
  online       BOOLEAN NOT NULL DEFAULT FALSE,
  last_seen_at TIMESTAMPTZ,
  fw_version   TEXT,
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- テレメトリ履歴（縦持ち：メトリクス名＋値）
CREATE TABLE IF NOT EXISTS telemetry (
  id         BIGSERIAL PRIMARY KEY,
  device_id  BIGINT NOT NULL REFERENCES devices(id),
  ts         TIMESTAMPTZ NOT NULL,
  metric     TEXT NOT NULL,
  value      DOUBLE PRECISION NOT NULL,
  unit       TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS telemetry_device_ts_idx     ON telemetry(device_id, ts DESC);
CREATE INDEX IF NOT EXISTS telemetry_device_metric_idx ON telemetry(device_id, metric, ts DESC);

-- デバイスイベントログ
CREATE TABLE IF NOT EXISTS device_events (
  id        BIGSERIAL PRIMARY KEY,
  device_id BIGINT NOT NULL REFERENCES devices(id),
  ts        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  level     TEXT NOT NULL DEFAULT 'INFO',
  message   TEXT NOT NULL,
  meta      JSONB
);
