-- 002_challenges.sql

CREATE TYPE goal_type AS ENUM ('daily_steps', 'weekly_distance_km', 'weekly_sessions');
CREATE TYPE challenge_status AS ENUM ('open', 'active', 'grace', 'settling', 'settled', 'cancelled');
CREATE TYPE enrollment_status AS ENUM ('active', 'completed', 'failed', 'withdrawn');

CREATE TABLE challenges (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    creator_id   BIGINT NOT NULL REFERENCES users(id),
    title        TEXT NOT NULL,
    sport        TEXT NOT NULL,
    goal_type    goal_type NOT NULL,
    goal_value   NUMERIC NOT NULL CHECK (goal_value > 0),
    source       TEXT NOT NULL,                -- strava | google_fit | apple_health
    stake_points BIGINT NOT NULL CHECK (stake_points > 0),
    fee_bps      INT NOT NULL DEFAULT 1000 CHECK (fee_bps BETWEEN 0 AND 10000),
    pass_ratio   NUMERIC NOT NULL DEFAULT 0.8 CHECK (pass_ratio > 0 AND pass_ratio <= 1),
    start_at     TIMESTAMPTZ NOT NULL,
    end_at       TIMESTAMPTZ NOT NULL,
    grace_hours  INT NOT NULL DEFAULT 48,
    status       challenge_status NOT NULL DEFAULT 'open',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_at > start_at)
);

-- Index phục vụ settlement job quét theo trạng thái + thời gian
CREATE INDEX idx_challenges_status_end ON challenges (status, end_at);

CREATE TABLE enrollments (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    challenge_id BIGINT NOT NULL REFERENCES challenges(id),
    user_id      BIGINT NOT NULL REFERENCES users(id),
    status       enrollment_status NOT NULL DEFAULT 'active',
    stake_txn_id BIGINT NOT NULL REFERENCES ledger_transactions(id),
    result       JSONB,
    settled_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (challenge_id, user_id)
);

CREATE INDEX idx_enrollments_user ON enrollments (user_id, created_at DESC);

-- Tiến độ materialize theo kỳ. Bảng update-heavy: fillfactor 70 để ăn HOT update
-- (không index nào chứa achieved/passed/updated_at).
CREATE TABLE enrollment_periods (
    enrollment_id BIGINT NOT NULL REFERENCES enrollments(id),
    period_start  DATE NOT NULL,
    period_end    DATE NOT NULL,               -- exclusive
    target        NUMERIC NOT NULL,
    achieved      NUMERIC NOT NULL DEFAULT 0,
    passed        BOOLEAN NOT NULL DEFAULT false,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (enrollment_id, period_start)
) WITH (fillfactor = 70);
