// Package api là HTTP layer cho mobile app KÈO: ví điểm, danh sách kèo,
// vào kèo, tiến độ, đổi thưởng. Auth để dạng hook (authUserID) cho khỏi
// buộc vào cơ chế session/JWT cụ thể.
package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hao/keo/challenge"
	"github.com/hao/keo/ledger"
	"github.com/hao/keo/reward"
)

// Catalog đổi thưởng. Skeleton để trong code; bản thật chuyển sang bảng
// shop_items có tồn kho + trạng thái.
var Catalog = map[string]struct {
	Name string
	Cost int64
}{
	// Tỷ giá 1 điểm = 1 VNĐ — cost xấp xỉ giá trị VNĐ của vật phẩm.
	"soap-sinh-duoc":      {Name: "Xà bông Sinh Dược", Cost: 39_000},
	"voucher-sport-500k":  {Name: "Voucher cửa hàng thể thao 500k", Cost: 480_000},
	"gear-trail-shoes":    {Name: "Giày chạy bộ trail", Cost: 2_500_000},
	"ticket-hn-marathon":  {Name: "Vé Marathon Hà Nội 2026 · 21km", Cost: 900_000},
	"ticket-sg-night-run": {Name: "Vé Night Run Sài Gòn · 10km", Cost: 600_000},
}

type Server struct {
	pool       *pgxpool.Pool
	ledger     *ledger.PGStore
	challenges *challenge.Store
	rewards    *reward.Service
	auth       func(*http.Request) (int64, error)
	adminAuth  func(*http.Request) (int64, error)
	jwtSecret  []byte
}

func NewServer(pool *pgxpool.Pool, l *ledger.PGStore, cs *challenge.Store,
	auth func(*http.Request) (int64, error), adminAuth func(*http.Request) (int64, error), jwtSecret []byte) *Server {
	return &Server{pool: pool, ledger: l, challenges: cs,
		rewards: reward.NewService(pool, l), auth: auth, adminAuth: adminAuth, jwtSecret: jwtSecret}
}

func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/wallet", s.withAuth(s.getWallet))
	mux.HandleFunc("GET /v1/wallet/transactions", s.withAuth(s.getTransactions))
	mux.HandleFunc("GET /v1/challenges", s.withAuth(s.listChallenges))
	mux.HandleFunc("POST /v1/challenges", s.withAuth(s.createChallenge))
	mux.HandleFunc("POST /v1/challenges/{id}/join", s.withAuth(s.joinChallenge))
	mux.HandleFunc("GET /v1/challenges/{id}/leaderboard", s.withAuth(s.leaderboard))
	mux.HandleFunc("GET /v1/me/challenges", s.withAuth(s.myChallenges))
	mux.HandleFunc("GET /v1/me/activities", s.withAuth(s.myActivities))
	mux.HandleFunc("GET /v1/me/stats", s.withAuth(s.myStats))
	mux.HandleFunc("POST /v1/redemptions", s.withAuth(s.redeem))
	mux.HandleFunc("GET /v1/redemptions", s.withAuth(s.listRedemptions))
	mux.HandleFunc("POST /v1/checkins", s.withAuth(s.postCheckin))
	mux.HandleFunc("GET /v1/rewards", s.withAuth(s.getRewards))
	mux.HandleFunc("GET /v1/shop", s.withAuth(s.shop))
	mux.HandleFunc("POST /v1/auth/zalo", s.zaloLogin)
	mux.HandleFunc("POST /v1/auth/zalo/verify", s.zaloVerify)
	mux.HandleFunc("GET /v1/charities/stats", s.withAuth(s.charitiesStats))

	// ===== Admin APIs =====
	mux.HandleFunc("GET /v1/admin/users", s.withAdminAuth(s.adminListUsers))
	mux.HandleFunc("POST /v1/admin/users/{id}/adjust", s.withAdminAuth(s.adminAdjustUserPoints))
	mux.HandleFunc("GET /v1/admin/redemptions", s.withAdminAuth(s.adminListRedemptions))
	mux.HandleFunc("POST /v1/admin/redemptions/{id}/status", s.withAdminAuth(s.adminUpdateRedemptionStatus))
	mux.HandleFunc("GET /v1/admin/shop-items", s.withAdminAuth(s.adminListShopItems))
	mux.HandleFunc("POST /v1/admin/shop-items", s.withAdminAuth(s.adminCreateShopItem))
	mux.HandleFunc("PUT /v1/admin/shop-items/{id}", s.withAdminAuth(s.adminUpdateShopItem))
	mux.HandleFunc("DELETE /v1/admin/shop-items/{id}", s.withAdminAuth(s.adminDeleteShopItem))
}

// RegisterDevRoutes gắn các endpoint CHỈ DÙNG KHI DEV (DEV_MODE=1):
// tạo user nhanh không cần hệ thống auth thật.
func (s *Server) RegisterDevRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			DisplayName string `json:"display_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.DisplayName == "" {
			httpError(w, http.StatusBadRequest, "cần display_name")
			return
		}
		var userID int64
		if err := s.pool.QueryRow(r.Context(),
			`INSERT INTO users (display_name) VALUES ($1) RETURNING id`,
			body.DisplayName,
		).Scan(&userID); err != nil {
			httpError(w, http.StatusInternalServerError, "create user")
			return
		}
		writeJSON(w, map[string]any{"user_id": userID, "display_name": body.DisplayName})
	})
}

func (s *Server) withAdminAuth(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := s.adminAuth(r)
		if err != nil {
			slog.Debug("admin auth thất bại", "err", err)
			httpError(w, http.StatusUnauthorized, "unauthorized - admin role required")
			return
		}
		h(w, r, userID)
	}
}

func (s *Server) shop(w http.ResponseWriter, r *http.Request, _ int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT sku, name, cost_points, stock, status
		FROM shop_items
		WHERE status = 'active'
		ORDER BY cost_points ASC`)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query shop items failed")
		return
	}
	defer rows.Close()

	type item struct {
		SKU   string `json:"sku"`
		Name  string `json:"name"`
		Cost  int64  `json:"cost"`
		Stock int    `json:"stock"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		var status string
		if err := rows.Scan(&it.SKU, &it.Name, &it.Cost, &it.Stock, &status); err != nil {
			httpError(w, http.StatusInternalServerError, "scan shop item failed")
			return
		}
		out = append(out, it)
	}
	writeJSON(w, out)
}

type handler func(w http.ResponseWriter, r *http.Request, userID int64)

func (s *Server) withAuth(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, err := s.auth(r)
		if err != nil {
			slog.Debug("auth thất bại", "err", err)
			httpError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		h(w, r, userID)
	}
}

// ===== Ví =====

func (s *Server) getWallet(w http.ResponseWriter, r *http.Request, userID int64) {
	// Một round-trip cho cả available + locked (trước là 2 lần Balance()).
	var available, locked int64
	err := s.pool.QueryRow(r.Context(), `
		SELECT
			COALESCE(SUM(b.balance) FILTER (WHERE a.type = 'user_available'), 0),
			COALESCE(SUM(b.balance) FILTER (WHERE a.type = 'user_locked'), 0)
		FROM ledger_accounts a
		LEFT JOIN account_balances b ON b.account_id = a.id
		WHERE a.user_id = $1 AND a.type IN ('user_available', 'user_locked')`,
		userID,
	).Scan(&available, &locked)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "read balance")
		return
	}
	writeJSON(w, map[string]int64{"available": available, "locked": locked})
}

func (s *Server) getTransactions(w http.ResponseWriter, r *http.Request, userID int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT t.id, t.type, t.created_at,
		       COALESCE(SUM(e.amount) FILTER (WHERE a.type = 'user_available'), 0) AS delta_available,
		       COALESCE(SUM(e.amount) FILTER (WHERE a.type = 'user_locked'), 0)    AS delta_locked
		FROM ledger_transactions t
		JOIN ledger_entries e ON e.txn_id = t.id
		JOIN ledger_accounts a ON a.id = e.account_id
		WHERE a.user_id = $1
		GROUP BY t.id, t.type, t.created_at
		ORDER BY t.id DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query")
		return
	}
	defer rows.Close()
	type txn struct {
		ID             int64     `json:"id"`
		Type           string    `json:"type"`
		CreatedAt      time.Time `json:"created_at"`
		DeltaAvailable int64     `json:"delta_available"`
		DeltaLocked    int64     `json:"delta_locked"`
	}
	out := []txn{}
	for rows.Next() {
		var t txn
		if err := rows.Scan(&t.ID, &t.Type, &t.CreatedAt, &t.DeltaAvailable, &t.DeltaLocked); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		out = append(out, t)
	}
	writeJSON(w, out)
}

// ===== Kèo =====

func (s *Server) listChallenges(w http.ResponseWriter, r *http.Request, userID int64) {
	// Trả cả 3 nhóm trạng thái (mở/đang chạy/kết thúc) cho FE nhóm hiển thị.
	// ORDER ưu tiên open→active/grace→ended để kèo còn tham gia được luôn ở đầu,
	// không bị kèo cũ đã kết thúc đẩy khỏi LIMIT. Kèm tên + cờ chủ kèo.
	rows, err := s.pool.Query(r.Context(), `
		SELECT c.id, c.title, c.sport, c.goal_type, c.goal_value, c.source,
		       c.stake_points, c.start_at, c.end_at, c.status,
		       COUNT(e.id) AS participants,
		       COUNT(e.id) FILTER (WHERE e.user_id = $1) > 0 AS joined,
		       c.max_participants, c.is_charity, c.charity_id,
		       u.display_name AS creator_name,
		       c.creator_id = $1 AS is_owner
		FROM challenges c
		JOIN users u ON u.id = c.creator_id
		LEFT JOIN enrollments e ON e.challenge_id = c.id
		WHERE c.status IN ('open', 'active', 'grace', 'settling', 'settled')
		GROUP BY c.id, u.display_name
		ORDER BY (CASE c.status WHEN 'open' THEN 0 WHEN 'active' THEN 1 WHEN 'grace' THEN 1 ELSE 2 END),
		         c.created_at DESC
		LIMIT 50`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query")
		return
	}
	defer rows.Close()
	type item struct {
		ID              int64     `json:"id"`
		Title           string    `json:"title"`
		Sport           string    `json:"sport"`
		GoalType        string    `json:"goal_type"`
		GoalValue       float64   `json:"goal_value"`
		Source          string    `json:"source"`
		StakePoints     int64     `json:"stake_points"`
		StartAt         time.Time `json:"start_at"`
		EndAt           time.Time `json:"end_at"`
		Status          string    `json:"status"`
		Participants    int64     `json:"participants"`
		Joined          bool      `json:"joined"`
		MaxParticipants int64     `json:"max_participants"`
		IsCharity       bool      `json:"is_charity"`
		CharityID       int64     `json:"charity_id"`
		CreatorName     string    `json:"creator_name"`
		IsOwner         bool      `json:"is_owner"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Title, &it.Sport, &it.GoalType, &it.GoalValue,
			&it.Source, &it.StakePoints, &it.StartAt, &it.EndAt, &it.Status,
			&it.Participants, &it.Joined, &it.MaxParticipants, &it.IsCharity, &it.CharityID,
			&it.CreatorName, &it.IsOwner); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		out = append(out, it)
	}
	writeJSON(w, out)
}

func (s *Server) createChallenge(w http.ResponseWriter, r *http.Request, userID int64) {
	var body struct {
		Title           string  `json:"title"`
		Sport           string  `json:"sport"`
		GoalType        string  `json:"goal_type"`
		GoalValue       float64 `json:"goal_value"`
		Source          string  `json:"source"`
		StakePoints     int64   `json:"stake_points"`
		DurationDays    int     `json:"duration_days"`
		MaxParticipants int     `json:"max_participants"`
		StartAt         string  `json:"start_at"`
		IsCharity       bool    `json:"is_charity"`
		CharityID       int64   `json:"charity_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.DurationDays < 1 || body.DurationDays > 90 {
		httpError(w, http.StatusBadRequest, "duration_days phải trong 1..90")
		return
	}
	// Kiểm tra số dư khả dụng trước khi tạo kèo
	bal, err := s.ledger.Balance(r.Context(), ledger.UserAvailable(userID))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "error checking balance")
		return
	}
	if bal < body.StakePoints {
		writeJoinError(w, ledger.ErrInsufficientBalance)
		return
	}

	var startAt time.Time
	if body.StartAt != "" {
		parsed, err := time.ParseInLocation("2006-01-02", body.StartAt, challenge.VNLocation)
		if err != nil {
			httpError(w, http.StatusBadRequest, "ngày bắt đầu không hợp lệ (định dạng YYYY-MM-DD)")
			return
		}
		startAt = parsed
	} else {
		nowVN := time.Now().In(challenge.VNLocation)
		startAt = time.Date(nowVN.Year(), nowVN.Month(), nowVN.Day(), 0, 0, 0, 0, challenge.VNLocation)
	}

	feeBps := int64(1000)
	if body.IsCharity {
		feeBps = 0 // miễn phí cho kèo từ thiện
	}

	// Tạo kèo + enroll người tạo NGUYÊN TỬ trong 1 tx — "chủ kèo" luôn cược vào
	// kèo của mình, không để lại kèo mồ côi nếu bước enroll lỗi.
	id, _, err := s.challenges.CreateWithCreator(r.Context(), challenge.Challenge{
		CreatorID: userID, Title: body.Title, Sport: body.Sport,
		GoalType: challenge.GoalType(body.GoalType), GoalValue: body.GoalValue,
		Source: body.Source, StakePoints: body.StakePoints,
		FeeBps: feeBps, PassRatio: 0.8,
		StartAt: startAt, EndAt: startAt.AddDate(0, 0, body.DurationDays), GraceHours: 48,
		MaxParticipants: body.MaxParticipants,
		IsCharity:       body.IsCharity,
		CharityID:       body.CharityID,
	})
	if err != nil {
		writeJoinError(w, err)
		return
	}
	writeJSON(w, map[string]int64{"challenge_id": id})
}

func (s *Server) joinChallenge(w http.ResponseWriter, r *http.Request, userID int64) {
	challengeID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "bad challenge id")
		return
	}
	enrollmentID, err := s.challenges.Join(r.Context(), challengeID, userID)
	if err != nil {
		writeJoinError(w, err)
		return
	}
	writeJSON(w, map[string]int64{"enrollment_id": enrollmentID})
}

func writeJoinError(w http.ResponseWriter, err error) {
	slog.Warn("join kèo lỗi", "err", err)
	switch {
	case errors.Is(err, ledger.ErrInsufficientBalance):
		httpError(w, http.StatusPaymentRequired, "không đủ điểm — mua thêm ở tab Ví")
	case errors.Is(err, challenge.ErrNotJoinable):
		httpError(w, http.StatusConflict, "kèo không còn nhận người")
	case errors.Is(err, challenge.ErrNotFound):
		httpError(w, http.StatusNotFound, "kèo không tồn tại")
	case errors.Is(err, challenge.ErrChallengeFull):
		httpError(w, http.StatusConflict, "kèo đã đầy người, không thể tham gia")
	default:
		httpError(w, http.StatusInternalServerError, "join failed")
	}
}

func (s *Server) myChallenges(w http.ResponseWriter, r *http.Request, userID int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT c.id, c.title, c.sport, c.source, c.goal_type, c.goal_value,
		       c.stake_points, c.start_at, c.end_at, c.status AS challenge_status,
		       e.status,
		       COUNT(p.*)                          AS periods_total,
		       COUNT(*) FILTER (WHERE p.passed)    AS periods_passed,
		       c.is_charity, c.charity_id
		FROM enrollments e
		JOIN challenges c ON c.id = e.challenge_id
		LEFT JOIN enrollment_periods p ON p.enrollment_id = e.id
		WHERE e.user_id = $1
		GROUP BY c.id, e.status
		ORDER BY c.end_at DESC LIMIT 50`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query")
		return
	}
	defer rows.Close()
	type item struct {
		ChallengeID     int64     `json:"challenge_id"`
		Title           string    `json:"title"`
		Sport           string    `json:"sport"`
		Source          string    `json:"source"`
		GoalType        string    `json:"goal_type"`
		GoalValue       float64   `json:"goal_value"`
		StakePoints     int64     `json:"stake_points"`
		StartAt         time.Time `json:"start_at"`
		EndAt           time.Time `json:"end_at"`
		ChallengeStatus string    `json:"challenge_status"`
		Status          string    `json:"status"`
		PeriodsTotal    int       `json:"periods_total"`
		PeriodsPassed   int       `json:"periods_passed"`
		IsCharity       bool      `json:"is_charity"`
		CharityID       int64     `json:"charity_id"`
	}
	out := []item{}
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ChallengeID, &it.Title, &it.Sport, &it.Source,
			&it.GoalType, &it.GoalValue, &it.StakePoints, &it.StartAt, &it.EndAt,
			&it.ChallengeStatus, &it.Status, &it.PeriodsTotal, &it.PeriodsPassed, &it.IsCharity, &it.CharityID); err != nil {
			httpError(w, http.StatusInternalServerError, "scan")
			return
		}
		out = append(out, it)
	}
	writeJSON(w, out)
}

// ===== Đổi thưởng =====

func (s *Server) redeem(w http.ResponseWriter, r *http.Request, userID int64) {
	var body struct {
		SKU         string          `json:"sku"`
		Fulfillment json.RawMessage `json:"fulfillment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}

	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "begin")
		return
	}
	defer tx.Rollback(ctx)

	// Lấy thông tin sản phẩm và lock row để tránh race condition về tồn kho (stock)
	var (
		itemName   string
		costPoints int64
		stock      int
		status     string
	)
	err = tx.QueryRow(ctx, `
		SELECT name, cost_points, stock, status
		FROM shop_items
		WHERE sku = $1 FOR UPDATE`,
		body.SKU,
	).Scan(&itemName, &costPoints, &stock, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		httpError(w, http.StatusNotFound, "sku không tồn tại")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query item failed")
		return
	}
	if status != "active" {
		httpError(w, http.StatusConflict, "sản phẩm không còn được mở bán")
		return
	}
	if stock <= 0 {
		httpError(w, http.StatusConflict, "sản phẩm đã hết hàng")
		return
	}

	// Lấy id trước để derive idempotency key, rồi insert với id tường minh —
	// redemptions.ledger_txn_id NOT NULL nên ledger phải post trước.
	var redemptionID int64
	if err := tx.QueryRow(ctx,
		`SELECT nextval(pg_get_serial_sequence('redemptions', 'id'))`,
	).Scan(&redemptionID); err != nil {
		httpError(w, http.StatusInternalServerError, "nextval")
		return
	}
	res, err := s.ledger.PostTx(ctx, tx, ledger.RedeemRequest(userID, costPoints, redemptionID))
	if err != nil {
		if errors.Is(err, ledger.ErrInsufficientBalance) {
			httpError(w, http.StatusPaymentRequired, "không đủ điểm")
			return
		}
		httpError(w, http.StatusInternalServerError, "ledger")
		return
	}
	
	// Convert json.RawMessage to string, fallback to '{}' if null
	fulfillmentStr := "{}"
	if len(body.Fulfillment) > 0 {
		fulfillmentStr = string(body.Fulfillment)
	}

	// Trừ tồn kho
	if _, err := tx.Exec(ctx, `
		UPDATE shop_items SET stock = stock - 1 WHERE sku = $1`,
		body.SKU,
	); err != nil {
		httpError(w, http.StatusInternalServerError, "update stock failed")
		return
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO redemptions (id, user_id, item_sku, cost_points, ledger_txn_id, fulfillment)
		OVERRIDING SYSTEM VALUE VALUES ($1, $2, $3, $4, $5, $6)`,
		redemptionID, userID, body.SKU, costPoints, res.TxnID, fulfillmentStr,
	); err != nil {
		httpError(w, http.StatusInternalServerError, "insert redemption")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpError(w, http.StatusInternalServerError, "commit")
		return
	}
	writeJSON(w, map[string]any{"redemption_id": redemptionID, "item": itemName, "cost": costPoints})
}

// ===== Helpers =====

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// listRedemptions: trả về danh sách quà đã đổi của user
func (s *Server) listRedemptions(w http.ResponseWriter, r *http.Request, userID int64) {
	ctx := r.Context()
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.item_sku, COALESCE(s.name, r.item_sku) as item_name, r.cost_points, r.status, r.created_at, r.fulfillment
		FROM redemptions r
		LEFT JOIN shop_items s ON r.item_sku = s.sku
		WHERE r.user_id = $1
		ORDER BY r.created_at DESC
		LIMIT 100`,
		userID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query redemptions failed")
		return
	}
	defer rows.Close()

	type redemptionResp struct {
		ID          int64     `json:"id"`
		ItemSKU     string    `json:"item_sku"`
		ItemName    string    `json:"item_name"`
		CostPoints  int64     `json:"cost_points"`
		Status      string    `json:"status"`
		CreatedAt   time.Time `json:"created_at"`
		Fulfillment any       `json:"fulfillment"`
	}

	var out []redemptionResp = []redemptionResp{}
	for rows.Next() {
		var rd redemptionResp
		err := rows.Scan(&rd.ID, &rd.ItemSKU, &rd.ItemName, &rd.CostPoints, &rd.Status, &rd.CreatedAt, &rd.Fulfillment)
		if err != nil {
			httpError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		out = append(out, rd)
	}
	writeJSON(w, out)
}

// charitiesStats trả về số dư (tổng đóng góp) của các quỹ từ thiện
func (s *Server) charitiesStats(w http.ResponseWriter, r *http.Request, userID int64) {
	// balance nằm ở bảng account_balances, KHÔNG phải cột của ledger_accounts —
	// phải LEFT JOIN (quỹ chưa nhận đồng nào thì chưa có row balance → COALESCE 0).
	rows, err := s.pool.Query(r.Context(), `
		SELECT a.user_id, COALESCE(b.balance, 0)
		FROM ledger_accounts a
		LEFT JOIN account_balances b ON b.account_id = a.id
		WHERE a.user_id IN (1001, 1002, 1003) AND a.type = 'user_available'
	`)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	out := map[string]int64{
		"1001": 0,
		"1002": 0,
		"1003": 0,
	}

	for rows.Next() {
		var cid, bal int64
		if err := rows.Scan(&cid, &bal); err != nil {
			httpError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
			return
		}
		out[fmt.Sprintf("%d", cid)] = bal
	}

	writeJSON(w, out)
}
