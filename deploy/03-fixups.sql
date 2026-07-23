-- Pha 3 — bước 3: chuẩn hoá danh tính SAU khi nạp dữ liệu Supabase.

-- 1. NULL hoá supabase_id UUID cũ (giữ nguyên "zalo:"/"google:"). User đăng nhập
--    Google cũ (supabase_id = UUID) sẽ tự re-link theo email đã verify ở lần đăng
--    nhập Google native ĐẦU TIÊN (syncSupabaseUser link khi supabase_id IS NULL +
--    email verified). Nếu KHÔNG chạy bước này → tạo tài khoản Google trùng, mất ví.
UPDATE users
SET supabase_id = NULL
WHERE supabase_id IS NOT NULL
  AND supabase_id !~ '^(zalo|google):';

-- 2. Cấp lại quyền admin — role cũ nằm trong Supabase auth (KHÔNG sang theo dump).
--    BỎ COMMENT dòng dưới và đổi email cho đúng tài khoản admin của bạn:
-- UPDATE users SET is_admin = true WHERE email = 'admin@example.com';

-- 3. Kiểm tra nhanh sau khi nạp:
--   SELECT count(*) FROM users;
--   SELECT count(*) FROM ledger_entries;
--   SELECT type, sum(...) ... -- đối soát sổ cái nếu cần
