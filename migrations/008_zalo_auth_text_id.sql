-- 008_zalo_auth_text_id.sql
-- Chuyển supabase_id sang TEXT để hỗ trợ cả Supabase UUID và Zalo ID (định dạng 'zalo:xxx')
ALTER TABLE users ALTER COLUMN supabase_id TYPE TEXT USING supabase_id::TEXT;
