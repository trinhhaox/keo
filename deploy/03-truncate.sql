-- Pha 3 — bước 1: xoá sạch dữ liệu SEED (charity users do app boot, shop_items do
-- migration 012) trước khi nạp dữ liệu THẬT từ Supabase. Giữ nguyên schema và
-- bảng schema_migrations. Chạy như superuser (user compose = superuser instance).
SET session_replication_role = replica;   -- tắt FK/trigger để truncate tự do

TRUNCATE
  account_balances,
  ledger_entries,
  ledger_transactions,
  ledger_accounts,
  enrollment_periods,
  enrollments,
  challenges,
  checkins,
  reward_events,
  reward_daily,
  activities,            -- bảng cha partition → cascade xuống mọi partition
  point_purchases,
  redemptions,
  shop_items,
  user_integrations,
  webhook_inbox,
  users
  RESTART IDENTITY CASCADE;

SET session_replication_role = DEFAULT;
