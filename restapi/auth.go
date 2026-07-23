package restapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// parseHMACClaims xác minh chữ ký HMAC (token app do backend tự ký cho Zalo/Google)
// và BẮT BUỘC có exp (token không exp = sống vĩnh viễn → chặn).
func parseHMACClaims(tokenStr string, secret []byte) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}

// claimName rút display name từ user_metadata (full_name ưu tiên, rồi name).
func claimName(claims jwt.MapClaims) string {
	meta, ok := claims["user_metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	if name, ok := meta["full_name"].(string); ok && name != "" {
		return name
	}
	if name, ok := meta["name"].(string); ok && name != "" {
		return name
	}
	return ""
}

// ValidateSupabaseJWT xác minh token app (HMAC, backend tự ký cho Zalo/Google) và
// trả internal userID. email_verified là claim NẰM TRONG token đã ký nên tin được
// để link user theo email (chỉ nguồn đã xác minh email — vd Google — mới set true).
func ValidateSupabaseJWT(ctx context.Context, tokenStr string, secret []byte, pool *pgxpool.Pool) (int64, error) {
	claims, err := parseHMACClaims(tokenStr, secret)
	if err != nil {
		return 0, err
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return 0, errors.New("missing sub in token")
	}
	email, _ := claims["email"].(string)
	emailVerified, _ := claims["email_verified"].(bool)
	displayName := claimName(claims)
	if displayName == "" {
		displayName = strings.Split(email, "@")[0]
	}
	return syncSupabaseUser(ctx, pool, sub, email, displayName, emailVerified, nil)
}

// syncSupabaseUser ánh xạ danh tính (sub) → internal user id, tạo user mới nếu chưa có.
//
// Nguyên tắc chống chiếm tài khoản:
//   - KHÔNG bao giờ gán đè supabase_id lên user đã gắn danh tính khác.
//   - Chỉ link vào user sẵn có theo email khi email đã được XÁC MINH — email chưa
//     verify là input attacker kiểm soát (đăng ký bằng email nạn nhân rồi chờ hệ
//     thống tự nối ví).
//   - Email chưa verify không được lưu — không cho chiếm chỗ trên UNIQUE(email).
//
// verify (optional) là hàm xác minh email LƯỜI qua nguồn quyền lực ngoài, chỉ gọi
// khi thực sự có khả năng link. Token app tự ký đã mang sẵn cờ email_verified tin
// cậy nên truyền verify=nil.
func syncSupabaseUser(ctx context.Context, pool *pgxpool.Pool,
	sub, email, displayName string, emailVerified bool, verify func(context.Context) bool) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx, `SELECT id FROM users WHERE supabase_id = $1`, sub).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("lookup by supabase_id: %w", err)
	}

	// Chỉ tới đây khi first-login (sub chưa có) → xác minh email đúng lúc. Cờ tin
	// cậy sẵn HOẶC xác minh lười qua verify.
	verified := emailVerified
	if !verified && verify != nil && email != "" {
		verified = verify(ctx)
	}

	// User sẵn có từ kênh khác (chưa gắn danh tính này) + email đã verify THẬT → link.
	if verified && email != "" {
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

	// Tạo user mới. ON CONFLICT (supabase_id): hai request đầu tiên của cùng user
	// đua nhau thì cùng hội tụ về một row.
	var storedEmail any
	if verified && email != "" {
		storedEmail = email
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO users (display_name, email, supabase_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (supabase_id) DO UPDATE SET supabase_id = EXCLUDED.supabase_id
		RETURNING id`, displayName, storedEmail, sub).Scan(&id)
	if isUniqueViolation(err) {
		// Email verified nhưng đã thuộc user gắn danh tính KHÁC — không cướp, tạo
		// user mới không email.
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

// AuthMiddleware parse token từ header Authorization → internal userID.
func AuthMiddleware(secret []byte, pool *pgxpool.Pool) func(*http.Request) (int64, error) {
	return func(r *http.Request) (int64, error) {
		tokenStr, err := bearerToken(r)
		if err != nil {
			return 0, err
		}
		return ValidateSupabaseJWT(r.Context(), tokenStr, secret, pool)
	}
}

// AdminMiddleware parse token + bắt buộc quyền admin (kiểm tra DB).
func AdminMiddleware(secret []byte, pool *pgxpool.Pool) func(*http.Request) (int64, error) {
	return func(r *http.Request) (int64, error) {
		tokenStr, err := bearerToken(r)
		if err != nil {
			return 0, err
		}
		return ValidateSupabaseAdminJWT(r.Context(), tokenStr, secret, pool)
	}
}

func bearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("missing authorization header")
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		return "", errors.New("invalid authorization format")
	}
	return parts[1], nil
}

// ValidateSupabaseAdminJWT xác minh token app rồi kiểm tra cột is_admin trong DB —
// nguồn quyền lực DUY NHẤT (không tin claim role trong token, tránh claim cũ còn
// hiệu lực sau khi thu hồi quyền).
func ValidateSupabaseAdminJWT(ctx context.Context, tokenStr string, secret []byte, pool *pgxpool.Pool) (int64, error) {
	userID, err := ValidateSupabaseJWT(ctx, tokenStr, secret, pool)
	if err != nil {
		return 0, err
	}
	var isAdmin bool
	if err := pool.QueryRow(ctx, `SELECT is_admin FROM users WHERE id = $1`, userID).Scan(&isAdmin); err != nil {
		return 0, fmt.Errorf("check admin: %w", err)
	}
	if !isAdmin {
		return 0, errors.New("unauthorized - admin role required")
	}
	return userID, nil
}
