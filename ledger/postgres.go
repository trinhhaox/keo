package ledger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore là implementation AlloyDB/PostgreSQL của Store.
//
// Toàn bộ Post chạy trong một DB transaction, READ COMMITTED là đủ vì:
//   - Idempotency dựa trên UNIQUE(idempotency_key), không dựa trên isolation.
//   - Chống chi âm dựa trên CHECK(balance >= 0), không dựa trên SELECT trước.
//   - Chống deadlock dựa trên thứ tự update cố định (sortedAccountKeys),
//     không dựa trên retry.
type PGStore struct {
	pool *pgxpool.Pool
}

func NewPGStore(pool *pgxpool.Pool) *PGStore {
	return &PGStore{pool: pool}
}

func (s *PGStore) Post(ctx context.Context, req Request) (Result, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) // no-op sau khi commit

	res, err := s.PostTx(ctx, tx, req)
	if err != nil {
		return Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}
	return res, nil
}

// PostTx post một giao dịch ledger TRONG transaction của caller — dùng khi
// nghiệp vụ cần ghi ledger + bảng khác một cách nguyên tử (ví dụ: insert
// enrollment + khóa cược khi vào kèo). Caller chịu trách nhiệm commit/rollback.
//
// Lưu ý: nếu trả về ErrInsufficientBalance thì tx của caller đã bị abort
// (CHECK violation) — caller bắt buộc rollback, không cố ghi tiếp.
func (s *PGStore) PostTx(ctx context.Context, tx pgx.Tx, req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}

	// 1. Insert transaction — đây là điểm quyết định idempotency.
	//    ON CONFLICT DO NOTHING + không có row trả về nghĩa là key đã tồn tại.
	meta, err := json.Marshal(req.Metadata)
	if err != nil {
		return Result{}, fmt.Errorf("marshal metadata: %w", err)
	}
	var txnID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO ledger_transactions (type, idempotency_key, metadata)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING id`,
		req.Type, req.IdempotencyKey, string(meta),
	).Scan(&txnID)

	if errors.Is(err, pgx.ErrNoRows) {
		// Replay: key đã tồn tại. Đọc txn cũ và trả về, KHÔNG ghi gì thêm.
		// (Txn cũ đã commit trọn vẹn vì entries + balance nằm cùng DB txn với nó.)
		//
		// QUAN TRỌNG: đọc trên CHÍNH connection của tx (tx.QueryRow), không phải
		// s.pool.QueryRow. Xin connection mới từ pool trong khi đang giữ tx
		// sẽ gây pool self-deadlock khi N goroutine đua cùng key: mọi conn
		// đều bị giữ bởi tx đang mở, ai cũng chờ conn mới → treo toàn bộ.
		// (Bug này bị bắt bởi TestIntegrationIdempotentReplay.)
		var existingID int64
		if err := tx.QueryRow(ctx,
			`SELECT id FROM ledger_transactions WHERE idempotency_key = $1`,
			req.IdempotencyKey,
		).Scan(&existingID); err != nil {
			return Result{}, fmt.Errorf("fetch replayed txn: %w", err)
		}
		return Result{TxnID: existingID, Replayed: true}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("insert txn: %w", err)
	}

	// 2. Resolve account key → id (tạo account nếu chưa có).
	//    DO UPDATE SET type = EXCLUDED.type là trick để RETURNING id hoạt động
	//    cả khi row đã tồn tại (DO NOTHING sẽ không trả row).
	keys := sortedAccountKeys(req.Entries)
	accountIDs := make(map[AccountKey]int64, len(keys))
	for _, k := range keys {
		var id int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO ledger_accounts (type, user_id, challenge_id)
			VALUES ($1, NULLIF($2, 0), NULLIF($3, 0))
			ON CONFLICT (type, user_id, challenge_id)
			DO UPDATE SET type = EXCLUDED.type
			RETURNING id`,
			k.Type, k.UserID, k.ChallengeID,
		).Scan(&id); err != nil {
			return Result{}, fmt.Errorf("resolve account %+v: %w", k, err)
		}
		accountIDs[k] = id
	}

	// 3. Insert entries theo batch.
	batch := &pgx.Batch{}
	for _, e := range req.Entries {
		batch.Queue(
			`INSERT INTO ledger_entries (txn_id, account_id, amount) VALUES ($1, $2, $3)`,
			txnID, accountIDs[e.Account], e.Amount,
		)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return Result{}, fmt.Errorf("insert entries: %w", err)
	}

	// 4. Cập nhật balance cache — gộp delta theo account, update theo thứ tự
	//    cố định (keys đã sort) để tránh deadlock giữa các txn song song.
	deltas := make(map[AccountKey]int64, len(keys))
	for _, e := range req.Entries {
		deltas[e.Account] += e.Amount
	}
	for _, k := range keys {
		delta := deltas[k]
		if delta == 0 {
			continue
		}
		// KHÔNG dùng INSERT ... VALUES ($delta) ON CONFLICT DO UPDATE ở đây:
		// PostgreSQL kiểm tra CHECK constraint trên TUPLE ĐỀ XUẤT INSERT trước
		// khi phát hiện conflict, nên mọi delta âm sẽ fail CHECK(balance >= 0)
		// bất kể số dư hiện tại là bao nhiêu → mọi giao dịch TRỪ điểm đều chết,
		// mọi giao dịch CỘNG điểm đều chạy. (Bug này bị bắt bởi
		// TestIntegrationNoNegativeBalance sau khi siết assertion success >= 1.)
		// Fix: đảm bảo row tồn tại với balance 0, rồi UPDATE cộng delta —
		// CHECK lúc này đánh giá trên kết quả thật.
		if _, err := tx.Exec(ctx, `
			INSERT INTO account_balances (account_id, balance, allow_negative)
			VALUES ($1, 0, $2)
			ON CONFLICT (account_id) DO NOTHING`,
			accountIDs[k], allowNegative(k.Type),
		); err != nil {
			return Result{}, fmt.Errorf("ensure balance row %+v: %w", k, err)
		}
		_, err := tx.Exec(ctx, `
			UPDATE account_balances
			SET balance = balance + $2, updated_at = now()
			WHERE account_id = $1`,
			accountIDs[k], delta,
		)
		if err != nil {
			if isCheckViolation(err) {
				// CHECK(balance >= 0) fail → không đủ điểm. Đây là đường
				// chống race chính thức: hai request cùng tiêu một số dư,
				// một cái sẽ chết ở đây và rollback sạch sẽ.
				return Result{}, fmt.Errorf("account %+v: %w", k, ErrInsufficientBalance)
			}
			return Result{}, fmt.Errorf("update balance %+v: %w", k, err)
		}
	}

	return Result{TxnID: txnID}, nil
}

func (s *PGStore) Balance(ctx context.Context, key AccountKey) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(b.balance, 0)
		FROM ledger_accounts a
		LEFT JOIN account_balances b ON b.account_id = a.id
		WHERE a.type = $1
		  AND a.user_id IS NOT DISTINCT FROM NULLIF($2::bigint, 0)
		  AND a.challenge_id IS NOT DISTINCT FROM NULLIF($3::bigint, 0)`,
		key.Type, key.UserID, key.ChallengeID,
	).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil // account chưa từng có giao dịch
	}
	if err != nil {
		return 0, fmt.Errorf("read balance: %w", err)
	}
	return balance, nil
}

// allowNegative: account hệ thống dạng "đối ứng phát hành/đốt" âm là bình thường
// (point_sale âm dần theo tổng điểm đã bán ra).
func allowNegative(t AccountType) bool {
	return t == AccountPointSale || t == AccountRewardExpense
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
