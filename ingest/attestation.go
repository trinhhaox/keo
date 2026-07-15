package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/MicahParks/keyfunc/v2"
	"github.com/golang-jwt/jwt/v5"
)

const appCheckJWKSURL = "https://firebaseappcheck.googleapis.com/v1/jwks"

// FirebaseAppCheckVerifier kiểm tra tính hợp lệ của Device Attestation Token
// được Firebase cấp (cho cả Android Play Integrity và iOS App Attest).
type FirebaseAppCheckVerifier struct {
	appID  string
	jwks   *keyfunc.JWKS
	logger *slog.Logger
}

// NewFirebaseAppCheckVerifier khởi tạo verifier và kéo public keys từ Google.
func NewFirebaseAppCheckVerifier(appID string, logger *slog.Logger) (*FirebaseAppCheckVerifier, error) {
	if appID == "" {
		return nil, errors.New("FIREBASE_APP_ID is required for app check")
	}

	// Fetch JWKS để lấy public keys verify chữ ký token, có cache tự động
	options := keyfunc.Options{
		RefreshInterval: time.Hour,
		RefreshRateLimit: time.Minute * 5,
		RefreshTimeout:  time.Second * 10,
		RefreshErrorHandler: func(err error) {
			logger.Error("Failed to refresh Firebase App Check JWKS", "error", err)
		},
	}

	jwks, err := keyfunc.Get(appCheckJWKSURL, options)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch JWKS from %s: %w", appCheckJWKSURL, err)
	}

	return &FirebaseAppCheckVerifier{
		appID:  appID,
		jwks:   jwks,
		logger: logger,
	}, nil
}

func (v *FirebaseAppCheckVerifier) Verify(ctx context.Context, userID int64, tokenStr string) error {
	if tokenStr == "" {
		return errors.New("attestation token is missing")
	}

	// Token của Google thường có tiền tố "ey..."
	// Parse và verify token bằng JWKS
	token, err := jwt.Parse(tokenStr, v.jwks.Keyfunc)
	if err != nil {
		v.logger.Warn("App Check Token invalid", "error", err, "user_id", userID)
		return fmt.Errorf("invalid attestation token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return errors.New("invalid attestation claims")
	}

	// Kiểm tra aud (Audience) phải chứa Firebase App ID hoặc project info
	// Theo document của Firebase App Check:
	// The audience must be the project numbers/ids.
	// Firebase JWT "aud" is usually an array or string.
	audRaw, ok := claims["aud"]
	if !ok {
		return errors.New("token missing audience")
	}

	validAud := false
	switch aud := audRaw.(type) {
	case string:
		if strings.Contains(aud, v.appID) {
			validAud = true
		}
	case []interface{}:
		for _, a := range aud {
			if str, ok := a.(string); ok && strings.Contains(str, v.appID) {
				validAud = true
				break
			}
		}
	}

	if !validAud {
		v.logger.Warn("App Check Token audience mismatch", "got", audRaw, "want", v.appID)
		return errors.New("attestation token not for this app")
	}

	// Issuer thường là https://firebaseappcheck.googleapis.com/<PROJECT_NUMBER>
	issRaw, ok := claims["iss"].(string)
	if !ok || !strings.HasPrefix(issRaw, "https://firebaseappcheck.googleapis.com/") {
		return errors.New("invalid issuer")
	}

	return nil
}
