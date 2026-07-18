-- 014: retry có backoff + claim-then-process cho webhook_inbox.
--
-- Trước: mọi lỗi handle() → 'failed', và cron requeue MỌI 'failed' mỗi lần chạy
-- → event hỏng vĩnh viễn (dữ liệu lạ) bị retry vô tận, hammer Strava. Ngoài ra
-- fetch Strava nằm trong DB tx (giữ connection suốt lúc gọi API).
--
-- Giờ: lỗi TẠM THỜI (timeout/429/5xx) giữ 'pending' + next_attempt_at (backoff),
-- lỗi VĨNH VIỄN → 'failed'. Trạng thái 'processing' cho claim-then-process: đánh
-- dấu rồi commit (nhả lock), fetch NGOÀI tx, apply ở tx khác. claimed_at để
-- sweeper đưa event kẹt (crash giữa chừng) về lại 'pending'.
ALTER TABLE webhook_inbox ADD COLUMN IF NOT EXISTS attempts INT NOT NULL DEFAULT 0;
ALTER TABLE webhook_inbox ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE webhook_inbox ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;

-- Claim: pending + đã đến hạn, ưu tiên theo id.
DROP INDEX IF EXISTS idx_inbox_pending;
CREATE INDEX IF NOT EXISTS idx_inbox_due
    ON webhook_inbox (provider, next_attempt_at, id) WHERE status = 'pending';

-- Sweeper tìm event kẹt ở 'processing'.
CREATE INDEX IF NOT EXISTS idx_inbox_processing
    ON webhook_inbox (claimed_at) WHERE status = 'processing';
