package restapi

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

// googleJWKSURL là endpoint public keys của Google để verify chữ ký ID token.
const googleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

// JWKS của Google khởi tạo lười 1 lần (keyfunc tự spawn goroutine refresh nền).
var (
	googleJWKSOnce sync.Once
	googleJWKS     *keyfunc.JWKS
	googleJWKSErr  error
)

func getGoogleJWKS() (*keyfunc.JWKS, error) {
	googleJWKSOnce.Do(func() {
		googleJWKS, googleJWKSErr = keyfunc.Get(googleJWKSURL, keyfunc.Options{
			RefreshInterval:  time.Hour,
			RefreshRateLimit: 5 * time.Minute,
			RefreshTimeout:   10 * time.Second,
			RefreshErrorHandler: func(err error) {
				log.Printf("google JWKS refresh error: %v", err)
			},
		})
	})
	return googleJWKS, googleJWKSErr
}

// googleLogin nhận ID token (credential) từ Google Identity Services ở frontend,
// verify chữ ký + iss + aud qua JWKS công khai của Google, rồi phát JWT nội bộ.
// KHÔNG cần Client Secret: luồng ID token chỉ dựa vào khóa công khai của Google.
func (s *Server) googleLogin(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GOOGLE_OAUTH_CLIENT_ID")
	if clientID == "" {
		httpError(w, http.StatusInternalServerError, "Google OAuth chưa cấu hình (GOOGLE_OAUTH_CLIENT_ID)")
		return
	}

	var body struct {
		Credential string `json:"credential"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "bad json")
		return
	}
	if body.Credential == "" {
		httpError(w, http.StatusBadRequest, "credential không được rỗng")
		return
	}

	jwks, err := getGoogleJWKS()
	if err != nil {
		httpError(w, http.StatusBadGateway, "không lấy được khóa công khai của Google")
		return
	}

	// Parse verify chữ ký RS256 + bắt buộc exp (jwt tự kiểm exp/nbf/iat nếu có).
	token, err := jwt.Parse(body.Credential, jwks.Keyfunc, jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		httpError(w, http.StatusUnauthorized, "Google ID token không hợp lệ")
		return
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		httpError(w, http.StatusUnauthorized, "claims không hợp lệ")
		return
	}

	// aud PHẢI đúng Client ID của ta — chặn token cấp cho ứng dụng khác.
	if aud, _ := claims["aud"].(string); aud != clientID {
		httpError(w, http.StatusUnauthorized, "token không dành cho ứng dụng này")
		return
	}
	// iss phải là Google.
	if iss, _ := claims["iss"].(string); iss != "accounts.google.com" && iss != "https://accounts.google.com" {
		httpError(w, http.StatusUnauthorized, "issuer không hợp lệ")
		return
	}

	sub, _ := claims["sub"].(string)
	if sub == "" {
		httpError(w, http.StatusUnauthorized, "thiếu sub trong token")
		return
	}
	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	name, _ := claims["name"].(string)
	picture, _ := claims["picture"].(string)

	displayName := strings.TrimSpace(name)
	if displayName == "" && email != "" {
		displayName = strings.Split(email, "@")[0]
	}
	if displayName == "" {
		displayName = "Người dùng Google"
	} else if len(displayName) > 100 {
		displayName = displayName[:100]
	}
	avatarURL := strings.TrimSpace(picture)
	if !strings.HasPrefix(avatarURL, "https://") {
		avatarURL = "" // chỉ chấp nhận ảnh https
	}

	tokenString, err := s.mintAppToken(r.Context(), "google:"+sub, email, emailVerified, displayName, avatarURL)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to sign JWT")
		return
	}

	writeJSON(w, map[string]string{
		"access_token": tokenString,
		"token_type":   "Bearer",
	})
}
