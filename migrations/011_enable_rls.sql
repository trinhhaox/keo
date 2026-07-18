-- 011_enable_rls.sql
-- Kích hoạt Row-Level Security (RLS) cho toàn bộ các bảng của hệ thống
-- để bảo vệ cơ sở dữ liệu trên Supabase khỏi các truy cập trực tiếp từ client (PostgREST API).
-- Do backend Go kết nối bằng tài khoản postgres (Superuser/Owner), backend sẽ bypass RLS 
-- và hoạt động bình thường, trong khi các truy cập từ anon/authenticated roles sẽ bị từ chối mặc định.

ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE account_balances ENABLE ROW LEVEL SECURITY;
ALTER TABLE challenges ENABLE ROW LEVEL SECURITY;
ALTER TABLE enrollments ENABLE ROW LEVEL SECURITY;
ALTER TABLE enrollment_periods ENABLE ROW LEVEL SECURITY;
ALTER TABLE user_integrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_inbox ENABLE ROW LEVEL SECURITY;
ALTER TABLE activities ENABLE ROW LEVEL SECURITY;
ALTER TABLE point_purchases ENABLE ROW LEVEL SECURITY;
ALTER TABLE redemptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE checkins ENABLE ROW LEVEL SECURITY;
ALTER TABLE reward_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE reward_daily ENABLE ROW LEVEL SECURITY;
