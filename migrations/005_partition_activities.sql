-- 005_partition_activities.sql

-- Đổi tên bảng cũ
ALTER TABLE activities RENAME TO activities_old;
ALTER INDEX idx_act_recompute RENAME TO idx_act_recompute_old;

-- Tạo bảng mới với PARTITION BY RANGE
CREATE TABLE activities (
    id                   BIGINT GENERATED ALWAYS AS IDENTITY,
    user_id              BIGINT NOT NULL REFERENCES users(id),
    source               TEXT NOT NULL,
    external_activity_id TEXT NOT NULL,
    sport                TEXT NOT NULL,
    distance_m           NUMERIC NOT NULL DEFAULT 0,
    duration_s           INT NOT NULL DEFAULT 0,
    steps                INT NOT NULL DEFAULT 0,
    sessions             INT NOT NULL DEFAULT 1,
    avg_heartrate        NUMERIC,
    is_manual_entry      BOOLEAN NOT NULL DEFAULT false,
    started_at           TIMESTAMPTZ NOT NULL,
    vn_date              DATE NOT NULL,
    raw                  JSONB NOT NULL DEFAULT '{}',
    ingested_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Ràng buộc PK và UNIQUE phải chứa cột partition key (started_at)
    PRIMARY KEY (id, started_at),
    UNIQUE (source, external_activity_id, started_at)
) PARTITION BY RANGE (started_at);

-- Tạo Index
CREATE INDEX idx_act_recompute ON activities (user_id, sport, source, vn_date);

-- Tạo các partition cho vài năm (ví dụ 2024 đến 2026) theo từng tháng.
-- Tạo partition DEFAULT cho các dòng lọt ngoài khoảng trên.
CREATE TABLE activities_default PARTITION OF activities DEFAULT;

CREATE TABLE activities_2024_01 PARTITION OF activities FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
CREATE TABLE activities_2024_02 PARTITION OF activities FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');
CREATE TABLE activities_2024_03 PARTITION OF activities FOR VALUES FROM ('2024-03-01') TO ('2024-04-01');
CREATE TABLE activities_2024_04 PARTITION OF activities FOR VALUES FROM ('2024-04-01') TO ('2024-05-01');
CREATE TABLE activities_2024_05 PARTITION OF activities FOR VALUES FROM ('2024-05-01') TO ('2024-06-01');
CREATE TABLE activities_2024_06 PARTITION OF activities FOR VALUES FROM ('2024-06-01') TO ('2024-07-01');
CREATE TABLE activities_2024_07 PARTITION OF activities FOR VALUES FROM ('2024-07-01') TO ('2024-08-01');
CREATE TABLE activities_2024_08 PARTITION OF activities FOR VALUES FROM ('2024-08-01') TO ('2024-09-01');
CREATE TABLE activities_2024_09 PARTITION OF activities FOR VALUES FROM ('2024-09-01') TO ('2024-10-01');
CREATE TABLE activities_2024_10 PARTITION OF activities FOR VALUES FROM ('2024-10-01') TO ('2024-11-01');
CREATE TABLE activities_2024_11 PARTITION OF activities FOR VALUES FROM ('2024-11-01') TO ('2024-12-01');
CREATE TABLE activities_2024_12 PARTITION OF activities FOR VALUES FROM ('2024-12-01') TO ('2025-01-01');

CREATE TABLE activities_2025_01 PARTITION OF activities FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');
CREATE TABLE activities_2025_02 PARTITION OF activities FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
CREATE TABLE activities_2025_03 PARTITION OF activities FOR VALUES FROM ('2025-03-01') TO ('2025-04-01');
CREATE TABLE activities_2025_04 PARTITION OF activities FOR VALUES FROM ('2025-04-01') TO ('2025-05-01');
CREATE TABLE activities_2025_05 PARTITION OF activities FOR VALUES FROM ('2025-05-01') TO ('2025-06-01');
CREATE TABLE activities_2025_06 PARTITION OF activities FOR VALUES FROM ('2025-06-01') TO ('2025-07-01');
CREATE TABLE activities_2025_07 PARTITION OF activities FOR VALUES FROM ('2025-07-01') TO ('2025-08-01');
CREATE TABLE activities_2025_08 PARTITION OF activities FOR VALUES FROM ('2025-08-01') TO ('2025-09-01');
CREATE TABLE activities_2025_09 PARTITION OF activities FOR VALUES FROM ('2025-09-01') TO ('2025-10-01');
CREATE TABLE activities_2025_10 PARTITION OF activities FOR VALUES FROM ('2025-10-01') TO ('2025-11-01');
CREATE TABLE activities_2025_11 PARTITION OF activities FOR VALUES FROM ('2025-11-01') TO ('2025-12-01');
CREATE TABLE activities_2025_12 PARTITION OF activities FOR VALUES FROM ('2025-12-01') TO ('2026-01-01');

CREATE TABLE activities_2026_01 PARTITION OF activities FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE activities_2026_02 PARTITION OF activities FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE activities_2026_03 PARTITION OF activities FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE activities_2026_04 PARTITION OF activities FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE activities_2026_05 PARTITION OF activities FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE activities_2026_06 PARTITION OF activities FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE activities_2026_07 PARTITION OF activities FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE activities_2026_08 PARTITION OF activities FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE activities_2026_09 PARTITION OF activities FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE activities_2026_10 PARTITION OF activities FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE activities_2026_11 PARTITION OF activities FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE activities_2026_12 PARTITION OF activities FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- Copy dữ liệu từ bảng cũ sang
INSERT INTO activities (
    user_id, source, external_activity_id, sport, distance_m, duration_s,
    steps, sessions, avg_heartrate, is_manual_entry, started_at, vn_date,
    raw, ingested_at
)
SELECT
    user_id, source, external_activity_id, sport, distance_m, duration_s,
    steps, sessions, avg_heartrate, is_manual_entry, started_at, vn_date,
    raw, ingested_at
FROM activities_old;

-- Xóa bảng cũ (đã copy xong)
DROP TABLE activities_old;
