-- 013: index user_id cho ledger_accounts.
-- UNIQUE(type, user_id, challenge_id) KHÔNG dùng được để lọc theo user_id vì
-- user_id không phải cột dẫn đầu. getWallet, getTransactions và charitiesStats
-- đều lọc a.user_id → thiếu index này là seq scan toàn bảng ledger_accounts.
CREATE INDEX IF NOT EXISTS idx_ledger_accounts_user_id ON ledger_accounts (user_id);
