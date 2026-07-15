-- 007_supabase_auth.sql
-- Thêm cột ánh xạ Supabase UUID

ALTER TABLE users ADD COLUMN IF NOT EXISTS supabase_id UUID UNIQUE;
