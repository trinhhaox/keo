package restapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hao/keo/ledger"
)

// adminListUsers trả về danh sách toàn bộ người dùng kèm số dư khả dụng và đóng băng của họ.
func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request, _ int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT u.id, u.display_name, COALESCE(u.email, ''), u.created_at,
		       COALESCE(ba.balance, 0) AS balance_available,
		       COALESCE(bl.balance, 0) AS balance_locked
		FROM users u
		LEFT JOIN ledger_accounts aa ON aa.user_id = u.id AND aa.type = 'user_available'
		LEFT JOIN account_balances ba ON ba.account_id = aa.id
		LEFT JOIN ledger_accounts al ON al.user_id = u.id AND al.type = 'user_locked'
		LEFT JOIN account_balances bl ON bl.account_id = al.id
		ORDER BY u.id DESC LIMIT 100`)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query users failed")
		return
	}
	defer rows.Close()

	type userItem struct {
		ID               int64     `json:"id"`
		DisplayName      string    `json:"display_name"`
		Email            string    `json:"email"`
		CreatedAt        time.Time `json:"created_at"`
		BalanceAvailable int64     `json:"balance_available"`
		BalanceLocked    int64     `json:"balance_locked"`
	}

	out := []userItem{}
	for rows.Next() {
		var u userItem
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email, &u.CreatedAt, &u.BalanceAvailable, &u.BalanceLocked); err != nil {
			httpError(w, http.StatusInternalServerError, "scan user failed")
			return
		}
		out = append(out, u)
	}
	writeJSON(w, out)
}

// adminAdjustUserPoints cộng/trừ điểm thủ công cho một user.
func (s *Server) adminAdjustUserPoints(w http.ResponseWriter, r *http.Request, _ int64) {
	targetUserID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "target user id không hợp lệ")
		return
	}

	var body struct {
		Delta  int64  `json:"delta"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.Delta == 0 {
		httpError(w, http.StatusBadRequest, "delta phải khác 0")
		return
	}
	if body.Reason == "" {
		body.Reason = "admin adjust"
	}

	// Đảm bảo user tồn tại trước khi post ledger
	var exists bool
	err = s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, targetUserID).Scan(&exists)
	if err != nil || !exists {
		httpError(w, http.StatusNotFound, "user không tồn tại")
		return
	}

	// Tạo ref key duy nhất để phục vụ idempotency
	refKey := fmt.Sprintf("%d_%s", time.Now().UnixNano(), body.Reason)

	_, err = s.ledger.Post(r.Context(), ledger.AdminAdjustRequest(targetUserID, body.Delta, refKey))
	if err != nil {
		if errors.Is(err, ledger.ErrInsufficientBalance) {
			httpError(w, http.StatusPaymentRequired, "không đủ điểm để trừ")
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, map[string]any{"success": true, "message": "điều chỉnh điểm thành công"})
}

// adminListRedemptions trả về danh sách đơn đổi quà của toàn bộ hệ thống.
func (s *Server) adminListRedemptions(w http.ResponseWriter, r *http.Request, _ int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT r.id, r.user_id, u.display_name, r.item_sku, r.cost_points, r.status, r.fulfillment, r.created_at
		FROM redemptions r
		JOIN users u ON u.id = r.user_id
		ORDER BY r.id DESC LIMIT 100`)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query redemptions failed")
		return
	}
	defer rows.Close()

	type redemptionItem struct {
		ID          int64           `json:"id"`
		UserID      int64           `json:"user_id"`
		UserDisplayName string      `json:"user_display_name"`
		ItemSKU     string          `json:"item_sku"`
		CostPoints  int64           `json:"cost_points"`
		Status      string          `json:"status"`
		Fulfillment json.RawMessage `json:"fulfillment"`
		CreatedAt   time.Time       `json:"created_at"`
	}

	out := []redemptionItem{}
	for rows.Next() {
		var r redemptionItem
		if err := rows.Scan(&r.ID, &r.UserID, &r.UserDisplayName, &r.ItemSKU, &r.CostPoints, &r.Status, &r.Fulfillment, &r.CreatedAt); err != nil {
			httpError(w, http.StatusInternalServerError, "scan redemption failed")
			return
		}
		out = append(out, r)
	}
	writeJSON(w, out)
}

// adminUpdateRedemptionStatus cập nhật trạng thái đơn quà tặng (fulfilled/cancelled/created).
func (s *Server) adminUpdateRedemptionStatus(w http.ResponseWriter, r *http.Request, _ int64) {
	redemptionID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "redemption id không hợp lệ")
		return
	}

	var body struct {
		Status      string          `json:"status"`
		Fulfillment json.RawMessage `json:"fulfillment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}

	fulfillmentStr := "{}"
	if len(body.Fulfillment) > 0 {
		fulfillmentStr = string(body.Fulfillment)
	}

	_, err = s.pool.Exec(r.Context(), `
		UPDATE redemptions
		SET status = $1, fulfillment = $2
		WHERE id = $3`,
		body.Status, fulfillmentStr, redemptionID,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to update redemption status")
		return
	}

	writeJSON(w, map[string]any{"success": true, "message": "cập nhật trạng thái đơn thành công"})
}

// adminListShopItems trả về tất cả các sản phẩm trong database bao gồm cả đang bán và ẩn.
func (s *Server) adminListShopItems(w http.ResponseWriter, r *http.Request, _ int64) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, sku, name, cost_points, stock, status, created_at
		FROM shop_items
		ORDER BY id DESC
		LIMIT 500`)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "query shop items failed")
		return
	}
	defer rows.Close()

	type shopItem struct {
		ID         int64     `json:"id"`
		SKU        string    `json:"sku"`
		Name       string    `json:"name"`
		CostPoints int64     `json:"cost"`
		Stock      int       `json:"stock"`
		Status     string    `json:"status"`
		CreatedAt  time.Time `json:"created_at"`
	}

	out := []shopItem{}
	for rows.Next() {
		var it shopItem
		if err := rows.Scan(&it.ID, &it.SKU, &it.Name, &it.CostPoints, &it.Stock, &it.Status, &it.CreatedAt); err != nil {
			httpError(w, http.StatusInternalServerError, "scan shop item failed")
			return
		}
		out = append(out, it)
	}
	writeJSON(w, out)
}

// adminCreateShopItem tạo mới một sản phẩm trong shop.
func (s *Server) adminCreateShopItem(w http.ResponseWriter, r *http.Request, _ int64) {
	var body struct {
		SKU        string `json:"sku"`
		Name       string `json:"name"`
		CostPoints int64  `json:"cost"`
		Stock      int    `json:"stock"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.SKU == "" || body.Name == "" || body.CostPoints <= 0 || body.Stock < 0 {
		httpError(w, http.StatusBadRequest, "thông tin sản phẩm không hợp lệ")
		return
	}
	if body.Status == "" {
		body.Status = "active"
	}

	var id int64
	err := s.pool.QueryRow(r.Context(), `
		INSERT INTO shop_items (sku, name, cost_points, stock, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`,
		body.SKU, body.Name, body.CostPoints, body.Stock, body.Status,
	).Scan(&id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to insert shop item (sku có thể đã tồn tại)")
		return
	}

	writeJSON(w, map[string]any{"success": true, "id": id, "message": "tạo sản phẩm thành công"})
}

// adminUpdateShopItem cập nhật thông tin sản phẩm.
func (s *Server) adminUpdateShopItem(w http.ResponseWriter, r *http.Request, _ int64) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "id sản phẩm không hợp lệ")
		return
	}

	var body struct {
		SKU        string `json:"sku"`
		Name       string `json:"name"`
		CostPoints int64  `json:"cost"`
		Stock      int    `json:"stock"`
		Status     string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}

	_, err = s.pool.Exec(r.Context(), `
		UPDATE shop_items
		SET sku = $1, name = $2, cost_points = $3, stock = $4, status = $5
		WHERE id = $6`,
		body.SKU, body.Name, body.CostPoints, body.Stock, body.Status, id,
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to update shop item")
		return
	}

	writeJSON(w, map[string]any{"success": true, "message": "cập nhật sản phẩm thành công"})
}

// adminDeleteShopItem xóa sản phẩm khỏi shop.
func (s *Server) adminDeleteShopItem(w http.ResponseWriter, r *http.Request, _ int64) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "id sản phẩm không hợp lệ")
		return
	}

	_, err = s.pool.Exec(r.Context(), `DELETE FROM shop_items WHERE id = $1`, id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to delete shop item")
		return
	}

	writeJSON(w, map[string]any{"success": true, "message": "xóa sản phẩm thành công"})
}
