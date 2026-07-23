-- 016: cột password_hash. Tồn tại trên DB Supabase cũ (tạo tay từ thời đầu — xem
-- ghi chú migration 000) nhưng chưa từng có trong migration. Bổ sung để:
--   (1) seed tài khoản quỹ (boot DDL) chạy được;
--   (2) nạp dữ liệu data-only từ Supabase không lỗi "thiếu cột".
-- Auth dùng OAuth (Zalo/Google) nên cột này luôn rỗng; để nullable cho an toàn khi
-- nạp dữ liệu cũ (giá trị có thể NULL).
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash text;
