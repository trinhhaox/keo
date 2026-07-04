package ledger

import (
	"context"
	"fmt"
)

// Store là cổng ra DB. Implementation duy nhất hiện tại là PGStore (postgres.go);
// interface tồn tại để test service logic bằng fake store nếu cần.
type Store interface {
	// Post ghi transaction + entries + cập nhật balance cache trong MỘT
	// DB transaction. Nếu idempotency key đã tồn tại, trả về txn cũ với
	// Replayed=true và không ghi gì thêm.
	Post(ctx context.Context, req Request) (Result, error)

	// Balance đọc số dư cache của một account. Account chưa tồn tại → 0.
	Balance(ctx context.Context, key AccountKey) (int64, error)
}

// Service bọc Store với validation. Mọi caller (API handler, settlement job,
// payment callback worker) đi qua đây, không gọi thẳng Store.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Post(ctx context.Context, req Request) (Result, error) {
	if err := req.Validate(); err != nil {
		return Result{}, err
	}
	res, err := s.store.Post(ctx, req)
	if err != nil {
		return Result{}, fmt.Errorf("ledger post %s (%s): %w", req.Type, req.IdempotencyKey, err)
	}
	return res, nil
}

func (s *Service) Balance(ctx context.Context, key AccountKey) (int64, error) {
	return s.store.Balance(ctx, key)
}

// ===== Convenience wrappers cho các use case chính =====

// Purchase ghi nhận user mua điểm (đã thanh toán thành công phía cổng thanh toán).
// Idempotency key nên derive từ (provider, provider_txn_id) để callback bắn trùng vô hại.
func (s *Service) Purchase(ctx context.Context, userID, points int64, provider, providerTxnID string) (Result, error) {
	return s.Post(ctx, PurchaseRequest(userID, points, provider, providerTxnID))
}

// StakeLock khóa điểm cược khi user vào kèo.
func (s *Service) StakeLock(ctx context.Context, userID, challengeID, stake int64) (Result, error) {
	return s.Post(ctx, StakeLockRequest(userID, challengeID, stake))
}

// Redeem trừ điểm khi đổi thưởng. redemptionID sinh trước ở tầng gọi
// (insert row redemptions trước, post ledger sau, cùng logic saga đơn giản).
func (s *Service) Redeem(ctx context.Context, userID, cost, redemptionID int64) (Result, error) {
	return s.Post(ctx, RedeemRequest(userID, cost, redemptionID))
}

// SettleChallenge chốt sổ một kèo. Toàn bộ chia thưởng là MỘT transaction —
// hoặc tất cả cùng nhận, hoặc không ai nhận.
func (s *Service) SettleChallenge(ctx context.Context, p SettlementParams) (Result, error) {
	req, err := SettlementRequest(p)
	if err != nil {
		return Result{}, err
	}
	return s.Post(ctx, req)
}
