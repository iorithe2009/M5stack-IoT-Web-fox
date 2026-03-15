CREATE TABLE IF NOT EXISTS commands (
  id            BIGSERIAL PRIMARY KEY,
  device_id     BIGINT NOT NULL REFERENCES devices(id),
  request_id    TEXT NOT NULL UNIQUE,
  command_type  TEXT NOT NULL,
  payload       JSONB NOT NULL DEFAULT '{}'::jsonb,
  status        TEXT NOT NULL,
  requested_by  TEXT NOT NULL DEFAULT 'web',
  error_message TEXT NOT NULL DEFAULT '',
  sent_at       TIMESTAMPTZ,
  ack_at        TIMESTAMPTZ,
  timeout_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS commands_device_created_idx
  ON commands(device_id, created_at DESC);

CREATE INDEX IF NOT EXISTS commands_status_idx
  ON commands(status, created_at DESC);
