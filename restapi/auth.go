package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidateSupabaseJWT kiểm tra chuỗi token, nếu hợp lệ sẽ trả về internal userID.
func ValidateSupabaseJWT(ctx context.Context, tokenStr string, secret []byte, pool *pgxpool.Pool) (int64, error) {
	var sub, email, displayName string
	var emailVerified bool
	
	// First try HMAC
	token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	if err == nil && token.Valid {
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return 0, errors.New("invalid claims")
		}
		sub, _ = claims["sub"].(string)
		email, _ = claims["email"].(string)
		if meta, ok := claims["user_metadata"].(map[string]interface{}); ok {
			if name, ok := meta["full_name"].(string); ok {
				displayName = name
			} else if name, ok := meta["name"].(string); ok {
				displayName = name
			}
			emailVerified, _ = meta["email_verified"].(bool)
		}
	} else {
		// Fallback to Supabase /auth/v1/user API for RS256
		supabaseURL := os.Getenv("SUPABASE_URL")
		if supabaseURL == "" {
			supabaseURL = os.Getenv("VITE_SUPABASE_URL") // fallback
		}
		if supabaseURL == "" {
			return 0, fmt.Errorf("token invalid and SUPABASE_URL not set: %v", err)
		}
		anonKey := os.Getenv("SUPABASE_ANON_KEY")
		if anonKey == "" {
			anonKey = os.Getenv("VITE_SUPABASE_ANON_KEY") // fallback
		}
		req, _ := http.NewRequestWithContext(ctx, "GET", supabaseURL+"/auth/v1/user", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		req.Header.Set("apikey", anonKey)
		
		resp, apiErr := http.DefaultClient.Do(req)
		if apiErr != nil {
			return 0, fmt.Errorf("supabase api error: %w", apiErr)
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != 200 {
			return 0, fmt.Errorf("supabase api returned status %d", resp.StatusCode)
		}
		
		var userResp struct {
			ID               string `json:"id"`
			Email            string `json:"email"`
			EmailConfirmedAt string `json:"email_confirmed_at"`
			Meta             struct {
				FullName string `json:"full_name"`
				Name     string `json:"name"`
			} `json:"user_metadata"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&userResp); err != nil {
			return 0, fmt.Errorf("decode supabase user: %w", err)
		}
		sub = userResp.ID
		email = userResp.Email
		emailVerified = userResp.EmailConfirmedAt != ""
		displayName = userResp.Meta.FullName
		if displayName == "" {
			displayName = userResp.Meta.Name
		}
	}

	if sub == "" {
		return 0, errors.New("missing sub in token")
	}
	if displayName == "" {
		displayName = strings.Split(email, "@")[0] // Fallback
	}

	return syncSupabaseUser(ctx, pool, sub, email, displayName, emailVerified)
}

// syncSupabaseUser ánh xạ danh tính Supabase (sub) → internal user id, tạo
// user mới nếu chưa có.
//
// Nguyên tắc chống chiếm tài khoản:
//   - KHÔNG bao giờ gán đè supabase_id lên user đã gắn Supabase khác.
//   - Chỉ link vào user sẵn có theo email khi email đã được Supabase XÁC MINH —
//     email chưa verify là input attacker kiểm soát (đăng ký bằng email nạn
//     nhân rồi chờ hệ thống tự nối ví).
//   - Email chưa verify không được lưu — không cho chiếm chỗ trên UNIQUE(email).
func syncSupabaseUser(ctx context.Context, pool *pgxpool.Pool,
	sub, email, displayName string, emailVerified bool) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE supabase_id = $1`, sub).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("lookup by supabase_id: %w", err)
	}

	// User sẵn có từ kênh khác (chưa gắn Supabase) + email đã verify → link.
	if emailVerified && email != "" {
		err = pool.QueryRow(ctx, `
			UPDATE users SET supabase_id = $1
			WHERE email = $2 AND supabase_id IS NULL
			RETURNING id`, sub, email).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("link by verified email: %w", err)
		}
	}

	// Tạo user mới. ON CONFLICT (supabase_id): hai request đầu tiên của cùng
	// user đua nhau thì cùng hội tụ về một row.
	var storedEmail any
	if emailVerified && email != "" {
		storedEmail = email
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO users (display_name, email, supabase_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (supabase_id) DO UPDATE SET supabase_id = EXCLUDED.supabase_id
		RETURNING id`, displayName, storedEmail, sub).Scan(&id)
	if isUniqueViolation(err) {
		// Email verified nhưng đã thuộc user gắn Supabase KHÁC — không cướp,
		// tạo user mới không email.
		err = pool.QueryRow(ctx, `
			INSERT INTO users (display_name, supabase_id) VALUES ($1, $2)
			ON CONFLICT (supabase_id) DO UPDATE SET supabase_id = EXCLUDED.supabase_id
			RETURNING id`, displayName, sub).Scan(&id)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to sync user: %w", err)
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// AuthMiddleware là hàm tiện ích để parse token từ header Authorization.
func AuthMiddleware(secret []byte, pool *pgxpool.Pool) func(*http.Request) (int64, error) {
	return func(r *http.Request) (int64, error) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			return 0, errors.New("missing authorization header")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return 0, errors.New("invalid authorization format")
		}

		return ValidateSupabaseJWT(r.Context(), parts[1], secret, pool)
	}
}
