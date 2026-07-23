-- 015: cột is_admin thay cho Supabase app_metadata.role.
-- Sau khi rời Supabase Auth, quyền admin không còn nằm trong schema auth của
-- Supabase nữa. DB trở thành nguồn quyền lực DUY NHẤT: middleware admin kiểm tra
-- trực tiếp cột này thay vì tin claim role trong JWT (tránh claim cũ còn hiệu lực
-- sau khi thu hồi quyền). Cấp admin bằng tay: UPDATE users SET is_admin=true WHERE ...
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin boolean NOT NULL DEFAULT false;
