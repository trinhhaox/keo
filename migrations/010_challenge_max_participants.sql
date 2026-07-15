-- 010_challenge_max_participants.sql
-- Code (challenge/store.go, restapi) đã dùng cột này từ trước nhưng thiếu
-- migration — DB mới dựng từ migrations sẽ vỡ ngay ở Create challenge.
-- 0 = không giới hạn số người tham gia.
ALTER TABLE challenges
    ADD COLUMN IF NOT EXISTS max_participants BIGINT NOT NULL DEFAULT 0;
