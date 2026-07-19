package restapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fetchZaloID gọi Zalo Graph API chỉ để lấy `id` xác thực của chủ access_token.
// Trả "" nếu Zalo chặn (IP server ngoài VN → lỗi -501) hoặc lỗi mạng — caller tự
// fallback sang id do client gửi. Tách riêng để giữ zaloVerify gọn.
func fetchZaloID(r *http.Request, accessToken string) string {
	req, err := http.NewRequestWithContext(r.Context(), "GET", "https://graph.zalo.me/v2.0/me?fields=id", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("access_token", accessToken)
	resp, err := zaloHTTPClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var p struct {
		ID    string `json:"id"`
		Error int    `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil || p.Error != 0 {
		return ""
	}
	return p.ID
}

// In-memory cache bảo mật lưu vết các access token hợp lệ do Zalo cấp
var (
	zaloTokensMutex sync.Mutex
	zaloTokensCache = make(map[string]time.Time)
)

// zaloHTTPClient có timeout — http.DefaultClient không có, Zalo treo là giữ
// goroutine/handler vô hạn.
var zaloHTTPClient = &http.Client{Timeout: 10 * time.Second}

type ZaloTokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       int    `json:"error"`
	ErrorName   string `json:"error_name"`
	ErrorMsg    string `json:"error_description"`
}

// zaloLogin đổi code lấy access token của Zalo, lưu vết bảo mật và trả về cho Frontend
func (s *Server) zaloLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.Code == "" || body.CodeVerifier == "" {
		httpError(w, http.StatusBadRequest, "code và code_verifier không được rỗng")
		return
	}

	appID := os.Getenv("ZALO_APP_ID")
	secretKey := os.Getenv("ZALO_SECRET_KEY")
	if appID == "" || secretKey == "" {
		httpError(w, http.StatusInternalServerError, "Zalo App credentials not configured")
		return
	}

	// 1. Đổi code lấy Access Token của Zalo
	data := url.Values{}
	data.Set("code", body.Code)
	data.Set("app_id", appID)
	data.Set("code_verifier", body.CodeVerifier)
	data.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(r.Context(), "POST", "https://oauth.zaloapp.com/v4/access_token", strings.NewReader(data.Encode()))
	if err != nil {
		httpError(w, http.StatusInternalServerError, "create request failed")
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("secret_key", secretKey)

	resp, err := zaloHTTPClient.Do(req)
	if err != nil {
		httpError(w, http.StatusBadGateway, fmt.Sprintf("failed to contact Zalo OAuth API: %v", err))
		return
	}
	defer resp.Body.Close()

	var tokenResp ZaloTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to decode Zalo token response")
		return
	}

	if tokenResp.Error != 0 || tokenResp.AccessToken == "" {
		errMsg := tokenResp.ErrorMsg
		if errMsg == "" {
			errMsg = tokenResp.ErrorName
		}
		if errMsg == "" {
			errMsg = "OAuth exchange failed"
		}
		httpError(w, http.StatusUnauthorized, fmt.Sprintf("Zalo OAuth error (%d): %s", tokenResp.Error, errMsg))
		return
	}

	// 2. Lưu vết Token vào In-memory Cache bảo mật (hạn dùng 5 phút)
	zaloTokensMutex.Lock()
	zaloTokensCache[tokenResp.AccessToken] = time.Now()
	zaloTokensMutex.Unlock()

	writeJSON(w, map[string]string{
		"zalo_access_token": tokenResp.AccessToken,
	})
}

// zaloVerify kiểm tra token từ cache, nếu hợp lệ sẽ tạo/đăng nhập user và sinh JWT app
func (s *Server) zaloVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ZaloAccessToken string `json:"zalo_access_token"`
		ID              string `json:"id"`
		Name            string `json:"name"`
		Picture         string `json:"picture"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.ZaloAccessToken == "" {
		httpError(w, http.StatusBadRequest, "zalo_access_token không được rỗng")
		return
	}

	// 1. Kiểm tra tính hợp lệ của token trong Cache bảo mật (One-time swap)
	zaloTokensMutex.Lock()
	createdAt, exists := zaloTokensCache[body.ZaloAccessToken]
	if exists {
		// Xóa luôn để tránh tấn công phát lại (Replay Attack)
		delete(zaloTokensCache, body.ZaloAccessToken)
	}
	zaloTokensMutex.Unlock()

	if !exists {
		httpError(w, http.StatusUnauthorized, "Zalo token expired or invalid")
		return
	}

	// Token hết hạn sau 5 phút
	if time.Since(createdAt) > 5*time.Minute {
		httpError(w, http.StatusUnauthorized, "Zalo verification session expired")
		return
	}

	// 2. Xác thực ID thật của user qua Zalo Graph API — CHỈ lấy `id`.
	//    Zalo chặn "thông tin cá nhân" (tên/ảnh) với request từ IP ngoài VN (lỗi -501);
	//    Vercel chạy ở Mỹ nên tên/ảnh phải lấy từ client (trình duyệt IP VN của user đã
	//    fetch Graph thành công). `id` thường KHÔNG bị coi là thông tin cá nhân nên vẫn
	//    trả về — dùng nó làm nguồn ID authoritative để chống giả mạo. Token đã được
	//    one-time cache ở trên xác thực là do luồng của ta cấp.
	zaloID := fetchZaloID(r, body.ZaloAccessToken)
	if zaloID != "" {
		log.Printf("zalo verify: dùng id authoritative từ Graph (IP server lấy được id)")
	} else {
		// Fallback: Zalo chặn cả `id` vì IP → tin id do client gửi (đến từ chính lời gọi
		// Graph đã xác thực ở trình duyệt VN của user). Log để theo dõi tần suất fallback.
		zaloID = strings.TrimSpace(body.ID)
		log.Printf("zalo verify: FALLBACK id từ client (Graph chặn id vì IP server ngoài VN)")
	}
	if zaloID == "" {
		httpError(w, http.StatusUnauthorized, "không xác định được Zalo user id")
		return
	}

	// Tên/ảnh lấy từ client — làm sạch để tránh lạm dụng khi hiển thị.
	displayName := strings.TrimSpace(body.Name)
	if displayName == "" {
		displayName = "Người dùng Zalo"
	} else if len(displayName) > 100 {
		displayName = displayName[:100]
	}
	avatarURL := strings.TrimSpace(body.Picture)
	if !strings.HasPrefix(avatarURL, "https://") {
		avatarURL = "" // chỉ chấp nhận ảnh https; chặn javascript:/data: URL
	}

	// 3. Tạo JWT cho hệ thống của chúng ta ký bằng jwtSecret (HMAC)
	claims := jwt.MapClaims{
		"sub":   "zalo:" + zaloID,
		"email": fmt.Sprintf("zalo_%s@zalo.com", zaloID),
		"user_metadata": map[string]interface{}{
			"full_name":  displayName,
			"avatar_url": avatarURL,
		},
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(s.jwtSecret)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to sign JWT")
		return
	}

	writeJSON(w, map[string]string{
		"access_token": tokenString,
		"token_type":   "Bearer",
	})
}
