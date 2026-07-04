-- 003_ingestion.sql

CREATE TABLE user_integrations (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id),
    provider          TEXT NOT NULL,            -- strava | google_fit | apple_health
    external_user_id  TEXT NOT NULL,            -- Strava athlete_id...
    access_token_enc  BYTEA,                    -- mã hóa envelope (KMS), không plaintext
    refresh_token_enc BYTEA,
    token_expires_at  TIMESTAMPTZ,
    connected_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at        TIMESTAMPTZ,
    UNIQUE (user_id, provider),
    -- Lớp anti-cheat đầu tiên: một tài khoản Strava không gắn được cho 2 user.
    UNIQUE (provider, external_user_id)
);

-- Inbox pattern: webhook handler chỉ INSERT rồi trả 200 ngay (<2s theo yêu cầu
-- Strava). Worker xử lý sau. Dedup bằng UNIQUE — webhook bắn trùng vô hại.
CREATE TABLE webhook_inbox (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider     TEXT NOT NULL,
    dedup_key    TEXT NOT NULL,
    payload      JSONB NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',  -- pending|processed|failed
    error        TEXT,
    received_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    UNIQUE (provider, dedup_key)
);

CREATE INDEX idx_inbox_pending ON webhook_inbox (provider, id) WHERE status = 'pending';

-- Hoạt động đã xác thực từ mọi nguồn.
-- Prod: PARTITION BY RANGE (started_at) theo tháng — bảng append-heavy lớn nhất
-- hệ thống. Skeleton để bảng thường cho gọn; schema không đổi khi partition.
CREATE TABLE activities (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id              BIGINT NOT NULL REFERENCES users(id),
    source               TEXT NOT NULL,
    external_activity_id TEXT NOT NULL,
    sport                TEXT NOT NULL,           -- walk|run|swim|bike|gym
    distance_m           NUMERIC NOT NULL DEFAULT 0,
    duration_s           INT NOT NULL DEFAULT 0,
    steps                INT NOT NULL DEFAULT 0,
    sessions             INT NOT NULL DEFAULT 1,  -- Health/Fit bucket có thể gộp nhiều buổi/ngày
    avg_heartrate        NUMERIC,
    is_manual_entry      BOOLEAN NOT NULL DEFAULT false, -- Strava nhập tay → không tính
    started_at           TIMESTAMPTZ NOT NULL,
    vn_date              DATE NOT NULL,           -- ngày theo Asia/Ho_Chi_Minh, tính lúc ingest
    raw                  JSONB NOT NULL DEFAULT '{}',
    ingested_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Chống double-count: một hoạt động nguồn ngoài chỉ vào hệ thống một lần,
    -- webhook bắn trùng / user bấm sync liên tục đều vô hại.
    UNIQUE (source, external_activity_id)
);

CREATE INDEX idx_act_recompute ON activities (user_id, sport, source, vn_date);
