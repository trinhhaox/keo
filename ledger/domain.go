// Package ledger cài đặt sổ cái double-entry cho hệ thống điểm KÈO.
//
// Bất biến cốt lõi:
//  1. Mọi biến động điểm là một Transaction gồm >= 2 Entry có tổng bằng 0.
//  2. Mọi Transaction có idempotency key duy nhất — post lại trả về kết quả cũ.
//  3. Account của user không bao giờ âm (enforce bằng CHECK constraint ở DB,
//     không phải bằng SELECT-then-UPDATE ở tầng app).
package ledger

import (
	"errors"
	"fmt"
)

type AccountType string

const (
	AccountUserAvailable AccountType = "user_available"
	AccountUserLocked    AccountType = "user_locked"
	AccountChallengePool AccountType = "challenge_pool"
	AccountPlatformFee   AccountType = "platform_fee"
	AccountPointSale     AccountType = "point_sale"     // đối ứng phát hành điểm, được phép âm
	AccountRewardExpense AccountType = "reward_expense" // đối ứng đốt điểm khi đổi thưởng
)

type TxnType string

const (
	TxnPurchase     TxnType = "purchase"
	TxnStakeLock    TxnType = "stake_lock"
	TxnStakeRelease TxnType = "stake_release"
	TxnSettlement   TxnType = "settlement"
	TxnRedeem       TxnType = "redeem"
	TxnAdminAdjust  TxnType = "admin_adjust"
)

// AccountKey định danh logic một account. UserID/ChallengeID = 0 nghĩa là
// "không áp dụng" (map thành NULL ở tầng DB).
type AccountKey struct {
	Type        AccountType
	UserID      int64
	ChallengeID int64
}

func UserAvailable(userID int64) AccountKey {
	return AccountKey{Type: AccountUserAvailable, UserID: userID}
}

func UserLocked(userID int64) AccountKey {
	return AccountKey{Type: AccountUserLocked, UserID: userID}
}

func ChallengePool(challengeID int64) AccountKey {
	return AccountKey{Type: AccountChallengePool, ChallengeID: challengeID}
}

func PlatformFee() AccountKey   { return AccountKey{Type: AccountPlatformFee} }
func PointSale() AccountKey     { return AccountKey{Type: AccountPointSale} }
func RewardExpense() AccountKey { return AccountKey{Type: AccountRewardExpense} }

// Entry là một dòng bút toán. Amount dương = ghi có, âm = ghi nợ.
type Entry struct {
	Account AccountKey
	Amount  int64
}

// Request là một giao dịch chờ post.
type Request struct {
	Type           TxnType
	IdempotencyKey string
	Metadata       map[string]any
	Entries        []Entry
}

// Result trả về sau khi post.
type Result struct {
	TxnID    int64
	Replayed bool // true nếu idempotency key đã tồn tại — không có gì được ghi thêm
}

var (
	ErrInsufficientBalance = errors.New("ledger: insufficient balance")
	ErrUnbalanced          = errors.New("ledger: entries do not sum to zero")
	ErrInvalidRequest      = errors.New("ledger: invalid request")
)

// Validate kiểm tra bất biến của request trước khi chạm DB.
func (r Request) Validate() error {
	if r.IdempotencyKey == "" {
		return fmt.Errorf("%w: empty idempotency key", ErrInvalidRequest)
	}
	if r.Type == "" {
		return fmt.Errorf("%w: empty txn type", ErrInvalidRequest)
	}
	if len(r.Entries) < 2 {
		return fmt.Errorf("%w: need >= 2 entries, got %d", ErrInvalidRequest, len(r.Entries))
	}
	var sum int64
	for i, e := range r.Entries {
		if e.Amount == 0 {
			return fmt.Errorf("%w: entry %d has zero amount", ErrInvalidRequest, i)
		}
		if e.Account.Type == "" {
			return fmt.Errorf("%w: entry %d has empty account type", ErrInvalidRequest, i)
		}
		sum += e.Amount
	}
	if sum != 0 {
		return fmt.Errorf("%w: sum = %d", ErrUnbalanced, sum)
	}
	return nil
}
