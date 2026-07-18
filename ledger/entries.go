package ledger

import (
	"fmt"
	"sort"
)

// File này chứa các builder sinh bộ entries CÂN BẰNG cho từng nghiệp vụ.
// Toàn bộ "công thức tiền" của hệ thống nằm ở đây — pure function, test được
// không cần DB.

// PurchaseRequest: user mua điểm.
//
//	point_sale      -points   (phát hành)
//	user_available  +points
func PurchaseRequest(userID, points int64, provider, providerTxnID string) Request {
	return Request{
		Type:           TxnPurchase,
		IdempotencyKey: fmt.Sprintf("purchase:%s:%s", provider, providerTxnID),
		Metadata:       map[string]any{"user_id": userID, "provider": provider},
		Entries: []Entry{
			{Account: PointSale(), Amount: -points},
			{Account: UserAvailable(userID), Amount: +points},
		},
	}
}

// StakeLockRequest: khóa cược khi vào kèo.
//
//	user_available  -stake
//	user_locked     +stake
func StakeLockRequest(userID, challengeID, stake int64) Request {
	return Request{
		Type:           TxnStakeLock,
		IdempotencyKey: fmt.Sprintf("stake:challenge=%d:user=%d", challengeID, userID),
		Metadata:       map[string]any{"user_id": userID, "challenge_id": challengeID},
		Entries: []Entry{
			{Account: UserAvailable(userID), Amount: -stake},
			{Account: UserLocked(userID), Amount: +stake},
		},
	}
}

// StakeReleaseRequest: hoàn trả cược khi hủy kèo.
//
//	user_locked     -stake
//	user_available  +stake
func StakeReleaseRequest(userID, challengeID, stake int64) Request {
	return Request{
		Type:           TxnStakeRelease,
		IdempotencyKey: fmt.Sprintf("release:challenge=%d:user=%d", challengeID, userID),
		Metadata:       map[string]any{"user_id": userID, "challenge_id": challengeID},
		Entries: []Entry{
			{Account: UserLocked(userID), Amount: -stake},
			{Account: UserAvailable(userID), Amount: +stake},
		},
	}
}

// RedeemRequest: đổi thưởng, đốt điểm.
//
//	user_available  -cost
//	reward_expense  +cost
func RedeemRequest(userID, cost, redemptionID int64) Request {
	return Request{
		Type:           TxnRedeem,
		IdempotencyKey: fmt.Sprintf("redeem:%d", redemptionID),
		Metadata:       map[string]any{"user_id": userID, "redemption_id": redemptionID},
		Entries: []Entry{
			{Account: UserAvailable(userID), Amount: -cost},
			{Account: RewardExpense(), Amount: +cost},
		},
	}
}

// RewardPayoutRequest: phát điểm thưởng (check-in, quãng đường Strava) vào ví.
// refKey phải duy nhất cho một lần quy đổi — hiện là id của reward_event
// kích hoạt quy đổi, nên retry/replay vô hại.
//
//	reward_expense  -points   (account hệ thống, được phép âm)
//	user_available  +points
func RewardPayoutRequest(userID, points int64, refKey string) Request {
	return Request{
		Type:           TxnRewardPayout,
		IdempotencyKey: fmt.Sprintf("reward:user=%d:%s", userID, refKey),
		Metadata:       map[string]any{"user_id": userID, "ref": refKey},
		Entries: []Entry{
			{Account: RewardExpense(), Amount: -points},
			{Account: UserAvailable(userID), Amount: +points},
		},
	}
}

// SettlementParams là input đã chốt của một kèo hết hạn (sau grace period).
type SettlementParams struct {
	ChallengeID  int64
	StakePoints  int64
	FeeBps       int64   // 1000 = 10%
	CompletedIDs []int64 // user về đích
	FailedIDs    []int64 // user rớt kèo
}

// SettlementRequest sinh toàn bộ bút toán chốt sổ một kèo.
//
// Với mỗi user rớt:      user_locked -stake  → challenge_pool +stake
// Với mỗi user về đích:  user_locked -stake  → user_available +stake  (hoàn cược)
//
//	challenge_pool -share → user_available +share (thưởng)
//
// Cuối cùng:             challenge_pool -(fee+dư) → platform_fee +(fee+dư)
//
// Chia NGUYÊN, phần dư dồn vào fee — đảm bảo tổng entries = 0 tuyệt đối
// và challenge_pool về đúng 0 sau settlement.
//
// Edge cases:
//   - Không ai rớt  → pool = 0, ai cũng chỉ nhận lại cược. Hợp lệ.
//   - Không ai đậu  → toàn bộ pool về platform_fee. (Chính sách hoàn 50%
//     nếu muốn tử tế hơn thì sửa ở đây — một chỗ duy nhất.)
//   - Trùng user ở cả 2 danh sách → lỗi, caller phải đảm bảo disjoint.
func SettlementRequest(p SettlementParams) (Request, error) {
	if p.StakePoints <= 0 {
		return Request{}, fmt.Errorf("%w: stake must be > 0", ErrInvalidRequest)
	}
	if p.FeeBps < 0 || p.FeeBps > 10000 {
		return Request{}, fmt.Errorf("%w: fee_bps out of range", ErrInvalidRequest)
	}
	seen := make(map[int64]bool, len(p.CompletedIDs)+len(p.FailedIDs))
	for _, id := range append(append([]int64{}, p.CompletedIDs...), p.FailedIDs...) {
		if seen[id] {
			return Request{}, fmt.Errorf("%w: user %d appears twice in settlement", ErrInvalidRequest, id)
		}
		seen[id] = true
	}

	pool := p.StakePoints * int64(len(p.FailedIDs))
	fee := pool * p.FeeBps / 10000
	distributable := pool - fee

	var share, remainder int64
	if n := int64(len(p.CompletedIDs)); n > 0 {
		share = distributable / n
		remainder = distributable - share*n
	} else {
		// Không ai về đích: toàn bộ distributable dồn vào fee.
		remainder = distributable
	}
	feeTotal := fee + remainder

	entries := make([]Entry, 0, 2*len(p.FailedIDs)+3*len(p.CompletedIDs)+2)

	for _, uid := range p.FailedIDs {
		entries = append(entries,
			Entry{Account: UserLocked(uid), Amount: -p.StakePoints},
			Entry{Account: ChallengePool(p.ChallengeID), Amount: +p.StakePoints},
		)
	}
	for _, uid := range p.CompletedIDs {
		entries = append(entries,
			Entry{Account: UserLocked(uid), Amount: -p.StakePoints},
			Entry{Account: UserAvailable(uid), Amount: +p.StakePoints},
		)
		if share > 0 {
			entries = append(entries,
				Entry{Account: ChallengePool(p.ChallengeID), Amount: -share},
				Entry{Account: UserAvailable(uid), Amount: +share},
			)
		}
	}
	if feeTotal > 0 {
		entries = append(entries,
			Entry{Account: ChallengePool(p.ChallengeID), Amount: -feeTotal},
			Entry{Account: PlatformFee(), Amount: +feeTotal},
		)
	}

	return Request{
		Type:           TxnSettlement,
		IdempotencyKey: fmt.Sprintf("settle:challenge=%d", p.ChallengeID),
		Metadata: map[string]any{
			"challenge_id": p.ChallengeID,
			"pool":         pool,
			"fee":          feeTotal,
			"share":        share,
			"completed":    len(p.CompletedIDs),
			"failed":       len(p.FailedIDs),
		},
		Entries: entries,
	}, nil
}

// sortedAccountKeys trả về danh sách account key duy nhất, sắp xếp ổn định.
// Dùng ở tầng store để lock/update balance theo THỨ TỰ CỐ ĐỊNH — hai settlement
// chạy song song chạm cùng tập account sẽ xếp hàng thay vì deadlock.
func sortedAccountKeys(entries []Entry) []AccountKey {
	uniq := map[AccountKey]bool{}
	for _, e := range entries {
		uniq[e.Account] = true
	}
	keys := make([]AccountKey, 0, len(uniq))
	for k := range uniq {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		return a.ChallengeID < b.ChallengeID
	})
	return keys
}

// AdminAdjustRequest: admin điều chỉnh điểm thủ công cho user.
// point_sale      -delta
// user_available  +delta
func AdminAdjustRequest(userID, delta int64, refKey string) Request {
	return Request{
		Type:           TxnAdminAdjust,
		IdempotencyKey: fmt.Sprintf("admin_adjust:user=%d:%s", userID, refKey),
		Metadata:       map[string]any{"user_id": userID, "ref": refKey},
		Entries: []Entry{
			{Account: PointSale(), Amount: -delta},
			{Account: UserAvailable(userID), Amount: +delta},
		},
	}
}

