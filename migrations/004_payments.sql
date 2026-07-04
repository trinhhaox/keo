-- 004_payments.sql

CREATE TABLE point_purchases (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id          BIGINT NOT NULL REFERENCES users(id),
    pack_points      BIGINT NOT NULL CHECK (pack_points > 0),
    bonus_points     BIGINT NOT NULL DEFAULT 0,
    price_vnd        BIGINT NOT NULL CHECK (price_vnd > 0),
    payment_provider TEXT NOT NULL,               -- 'zalopay' | 'momo'
    provider_txn_id  TEXT NOT NULL,               -- app_trans_id phía ZaloPay
    status           TEXT NOT NULL DEFAULT 'pending', -- pending|paid|failed
    ledger_txn_id    BIGINT REFERENCES ledger_transactions(id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    paid_at          TIMESTAMPTZ,
    -- Callback bắn trùng / race đều quy về đúng một đơn.
    UNIQUE (payment_provider, provider_txn_id)
);

CREATE INDEX idx_purchases_user ON point_purchases (user_id, created_at DESC);

CREATE TABLE redemptions (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id),
    item_sku      TEXT NOT NULL,
    cost_points   BIGINT NOT NULL CHECK (cost_points > 0),
    status        TEXT NOT NULL DEFAULT 'created', -- created|fulfilled|cancelled
    ledger_txn_id BIGINT NOT NULL REFERENCES ledger_transactions(id),
    fulfillment   JSONB NOT NULL DEFAULT '{}',     -- mã voucher, mã vận đơn, mã bib...
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_redemptions_user ON redemptions (user_id, created_at DESC);
