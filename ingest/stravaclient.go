package ingest

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ===== Mã hóa token =====

// TokenCipher mã hóa token trước khi chạm DB — access/refresh token Strava
// trong plaintext là quyền đọc toàn bộ lịch sử vận động của user.
// Prod nên nâng lên envelope encryption với Cloud KMS; AES-GCM key tĩnh
// (từ secret manager) là mức sàn chấp nhận được.
type TokenCipher interface {
	Encrypt(ctx context.Context, plaintext []byte) ([]byte, error)
	Decrypt(ctx context.Context, ciphertext []byte) ([]byte, error)
}

type AESGCMCipher struct{ aead cipher.AEAD }

func NewAESGCMCipher(key32 []byte) (*AESGCMCipher, error) {
	block, err := aes.NewCipher(key32)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &AESGCMCipher{aead: aead}, nil
}

func (c *AESGCMCipher) Encrypt(_ context.Context, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plaintext, nil), nil // nonce || ciphertext
}

func (c *AESGCMCipher) Decrypt(_ context.Context, ct []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(ct) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return c.aead.Open(nil, ct[:ns], ct[ns:], nil)
}

// ===== Strava API client =====

// HTTPStravaClient là implementation thật của StravaClient.
// BaseURL override được để test bằng httptest.
type HTTPStravaClient struct {
	Pool         *pgxpool.Pool
	Cipher       TokenCipher
	ClientID     string
	ClientSecret string
	BaseURL      string // mặc định https://www.strava.com
	HC           *http.Client
}

func (c *HTTPStravaClient) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return "https://www.strava.com"
}

func (c *HTTPStravaClient) hc() *http.Client {
	if c.HC != nil {
		return c.HC
	}
	return http.DefaultClient
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // unix
	Athlete      struct {
		ID int64 `json:"id"`
	} `json:"athlete"`
}

// ExchangeCode hoàn tất OAuth: đổi authorization code lấy token và lưu
// integration cho user. Gọi từ redirect handler sau khi user bấm
// "Ủy quyền truy cập" trên trang Strava.
func (c *HTTPStravaClient) ExchangeCode(ctx context.Context, userID int64, code string) error {
	tok, err := c.tokenCall(ctx, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"grant_type":    {"authorization_code"},
		"code":          {code},
	})
	if err != nil {
		return fmt.Errorf("exchange code: %w", err)
	}
	accessEnc, err := c.Cipher.Encrypt(ctx, []byte(tok.AccessToken))
	if err != nil {
		return err
	}
	refreshEnc, err := c.Cipher.Encrypt(ctx, []byte(tok.RefreshToken))
	if err != nil {
		return err
	}
	// UNIQUE(provider, external_user_id) sẽ chặn trường hợp tài khoản Strava
	// này đã gắn với user khác — trả lỗi rõ ràng thay vì âm thầm cướp quyền.
	_, err = c.Pool.Exec(ctx, `
		INSERT INTO user_integrations
			(user_id, provider, external_user_id, access_token_enc, refresh_token_enc, token_expires_at)
		VALUES ($1, 'strava', $2, $3, $4, to_timestamp($5))
		ON CONFLICT (user_id, provider) DO UPDATE SET
			external_user_id = EXCLUDED.external_user_id,
			access_token_enc = EXCLUDED.access_token_enc,
			refresh_token_enc = EXCLUDED.refresh_token_enc,
			token_expires_at = EXCLUDED.token_expires_at,
			revoked_at = NULL`,
		userID, fmt.Sprint(tok.Athlete.ID), accessEnc, refreshEnc, tok.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("store integration: %w", err)
	}
	return nil
}

// GetActivity fetch chi tiết hoạt động bằng token của athlete, tự refresh
// khi token sắp hết hạn.
func (c *HTTPStravaClient) GetActivity(ctx context.Context, athleteID, activityID int64) (StravaActivity, error) {
	token, err := c.accessToken(ctx, athleteID)
	if err != nil {
		return StravaActivity{}, err
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf("%s/api/v3/activities/%d", c.base(), activityID), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.hc().Do(req)
	if err != nil {
		return StravaActivity{}, fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return StravaActivity{}, fmt.Errorf("strava %d: %s", resp.StatusCode, b)
	}
	var body struct {
		ID           int64     `json:"id"`
		SportType    string    `json:"sport_type"`
		Type         string    `json:"type"` // field cũ, fallback
		Distance     float64   `json:"distance"`
		MovingTime   int       `json:"moving_time"`
		AvgHeartrate float64   `json:"average_heartrate"`
		Manual       bool      `json:"manual"`
		StartDate    time.Time `json:"start_date"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return StravaActivity{}, fmt.Errorf("decode: %w", err)
	}
	typ := body.SportType
	if typ == "" {
		typ = body.Type
	}
	return StravaActivity{
		ID:           body.ID,
		Type:         typ,
		DistanceM:    body.Distance,
		MovingTimeS:  body.MovingTime,
		AvgHeartrate: body.AvgHeartrate,
		Manual:       body.Manual,
		StartDate:    body.StartDate,
	}, nil
}

// accessToken trả về access token còn hạn của athlete, refresh nếu cần.
// Refresh ghi đè token mới vào DB — refresh token của Strava xoay vòng
// (token cũ bị vô hiệu sau khi dùng) nên BẮT BUỘC lưu lại ngay.
func (c *HTTPStravaClient) accessToken(ctx context.Context, athleteID int64) (string, error) {
	var (
		integrationID         int64
		accessEnc, refreshEnc []byte
		expiresAt             time.Time
	)
	err := c.Pool.QueryRow(ctx, `
		SELECT id, access_token_enc, refresh_token_enc, token_expires_at
		FROM user_integrations
		WHERE provider = 'strava' AND external_user_id = $1 AND revoked_at IS NULL`,
		fmt.Sprint(athleteID),
	).Scan(&integrationID, &accessEnc, &refreshEnc, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("athlete %d chưa kết nối", athleteID)
	}
	if err != nil {
		return "", fmt.Errorf("load integration: %w", err)
	}

	if time.Until(expiresAt) > time.Minute {
		access, err := c.Cipher.Decrypt(ctx, accessEnc)
		if err != nil {
			return "", fmt.Errorf("decrypt access: %w", err)
		}
		return string(access), nil
	}

	// Token hết hạn / sắp hết → refresh.
	refresh, err := c.Cipher.Decrypt(ctx, refreshEnc)
	if err != nil {
		return "", fmt.Errorf("decrypt refresh: %w", err)
	}
	tok, err := c.tokenCall(ctx, url.Values{
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {string(refresh)},
	})
	if err != nil {
		return "", fmt.Errorf("refresh token: %w", err)
	}
	newAccessEnc, err := c.Cipher.Encrypt(ctx, []byte(tok.AccessToken))
	if err != nil {
		return "", err
	}
	newRefreshEnc, err := c.Cipher.Encrypt(ctx, []byte(tok.RefreshToken))
	if err != nil {
		return "", err
	}
	if _, err := c.Pool.Exec(ctx, `
		UPDATE user_integrations
		SET access_token_enc = $1, refresh_token_enc = $2, token_expires_at = to_timestamp($3)
		WHERE id = $4`,
		newAccessEnc, newRefreshEnc, tok.ExpiresAt, integrationID,
	); err != nil {
		return "", fmt.Errorf("store refreshed token: %w", err)
	}
	return tok.AccessToken, nil
}

func (c *HTTPStravaClient) tokenCall(ctx context.Context, form url.Values) (tokenResponse, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base()+"/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.hc().Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return tokenResponse{}, fmt.Errorf("oauth %d: %s", resp.StatusCode, b)
	}
	var tok tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tokenResponse{}, err
	}
	return tok, nil
}
