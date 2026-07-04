-- 001_ledger.sql
-- Yêu cầu PostgreSQL 15+ (AlloyDB hiện hỗ trợ) vì dùng UNIQUE NULLS NOT DISTINCT.
--
-- LƯU Ý QUAN TRỌNG so với bản thiết kế ban đầu:
-- UNIQUE (type, user_id, challenge_id) mặc định coi NULL là "distinct",
-- nghĩa là có thể insert 2 account 'platform_fee' (user_id NULL, challenge_id NULL)
-- mà không bị chặn → vỡ bất biến "mỗi account key duy nhất một row".
-- Fix: NULLS NOT DISTINCT.

CREATE TYPE account_type AS ENUM (
    'user_available',
    'user_locked',
    'challenge_pool',
    'platform_fee',
    'point_sale',
    'reward_expense'
);

CREATE TYPE ledger_txn_type AS ENUM (
    'purchase', 'stake_lock', 'stake_release',
    'forfeit', 'reward_payout', 'settlement', 'redeem', 'admin_adjust'
);

CREATE TABLE ledger_accounts (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type         account_type NOT NULL,
    user_id      BIGINT REFERENCES users(id),
    challenge_id BIGINT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (type, user_id, challenge_id)
);

CREATE TABLE ledger_transactions (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    type            ledger_txn_type NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ledger_entries (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    txn_id     BIGINT NOT NULL REFERENCES ledger_transactions(id),
    account_id BIGINT NOT NULL REFERENCES ledger_accounts(id),
    amount     BIGINT NOT NULL CHECK (amount <> 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entries_account ON ledger_entries (account_id, id);
CREATE INDEX idx_entries_txn ON ledger_entries (txn_id);

-- Số dư cache. CHECK (balance >= 0) là chốt chặn cuối cùng chống chi âm
-- dưới concurrency: mọi race lọt qua tầng app đều bị DB reject (SQLSTATE 23514).
-- Account hệ thống (point_sale, reward_expense) được phép âm — xem cột allow_negative.
CREATE TABLE account_balances (
    account_id     BIGINT PRIMARY KEY REFERENCES ledger_accounts(id),
    balance        BIGINT NOT NULL DEFAULT 0,
    allow_negative BOOLEAN NOT NULL DEFAULT false,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (allow_negative OR balance >= 0)
);
