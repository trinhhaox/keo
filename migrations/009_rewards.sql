-- 009_rewards.sql
-- Hệ thống điểm thưởng: check-in hàng ngày +1 điểm, +1 điểm mỗi km đi bộ/
-- chạy bộ từ Strava. Tỷ giá hệ thống: 1 điểm = 1 VNĐ, thưởng là điểm NGUYÊN
-- nên cộng thẳng vào ví qua ledger txn 'reward_payout' (enum có sẵn từ 001) —
-- không cần tầng tích lũy lẻ.

-- Check-in hàng ngày. UNIQUE (user_id, vn_date) = idempotency: bấm bao nhiêu
-- lần trong ngày cũng chỉ ghi nhận một.
CREATE TABLE checkins (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    vn_date    DATE NOT NULL,           -- ngày theo Asia/Ho_Chi_Minh
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, vn_date)
);

-- Sự kiện thưởng — nguồn sự thật của mọi điểm thưởng đã cấp, và là điểm
-- quyết định idempotency: webhook bắn lại, user double-tap đều vô hại nhờ
-- UNIQUE (user_id, kind, ref_key). Ledger payout dùng key reward:user=N:event=M
-- nên một event chỉ phát điểm đúng một lần.
-- points = 0 hợp lệ: sự kiện đến khi đã kịch trần ngày vẫn được ghi để "đốt"
-- ref_key — replay/update sau không cấp lại được.
CREATE TABLE reward_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id),
    kind       TEXT NOT NULL,           -- checkin | activity_distance
    ref_key    TEXT NOT NULL,           -- checkin: '2026-07-16' | distance: 'strava:12345'
    points     BIGINT NOT NULL CHECK (points >= 0), -- số điểm THẬT đã cấp (sau clamp trần)
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind, ref_key)
);

CREATE INDEX idx_reward_events_user ON reward_events (user_id, id DESC);

-- Counter thưởng theo ngày (giờ VN): chỗ enforce TRẦN THƯỞNG/NGÀY, đồng thời
-- row lock của nó serialize các accrual song song của cùng user — hai request
-- cùng lúc không thể cùng lách trần. Trần cụ thể nằm ở tầng app (reward.DailyCap).
CREATE TABLE reward_daily (
    user_id BIGINT NOT NULL REFERENCES users(id),
    vn_date DATE NOT NULL,
    points  BIGINT NOT NULL DEFAULT 0 CHECK (points >= 0),
    PRIMARY KEY (user_id, vn_date)
);
