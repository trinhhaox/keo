-- 000_users.sql
-- Bảng gốc mọi migration sau tham chiếu tới. (Bị phát hiện thiếu khi đóng gói:
-- các phiên dev trước tạo bảng này bằng tay — DB trắng tinh sẽ vỡ ngay 001.)

CREATE TABLE users (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    phone        TEXT UNIQUE,
    display_name TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
