package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type contextKey string

const (
	ctxUserID contextKey = "user_id"
	ctxRole   contextKey = "role"
)

type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

func Auth(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(header, "Bearer ") {
				writeAuthError(w, "TOKEN_INVALID", "Токен отсутствует", http.StatusUnauthorized)
				return
			}

			tokenStr := strings.TrimPrefix(header, "Bearer ")
			claims := &Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, errors.New("unexpected signing method")
				}
				return []byte(secret), nil
			})

			if err != nil || !token.Valid {
				code, msg := "TOKEN_INVALID", "Невалидный токен"
				if errors.Is(err, jwt.ErrTokenExpired) {
					code, msg = "TOKEN_EXPIRED", "Токен истёк"
				}
				writeAuthError(w, code, msg, http.StatusUnauthorized)
				return
			}

			userID, parseErr := uuid.Parse(claims.Subject)
			if parseErr != nil {
				writeAuthError(w, "TOKEN_INVALID", "Невалидный sub в токене", http.StatusUnauthorized)
				return
			}

			setLogUserID(r, userID)

			ctx := context.WithValue(r.Context(), ctxUserID, userID)
			ctx = context.WithValue(ctx, ctxRole, claims.Role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromCtx(ctx context.Context) uuid.UUID {
	v, _ := ctx.Value(ctxUserID).(uuid.UUID)
	return v
}

func RoleFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(ctxRole).(string)
	return v
}

func writeAuthError(w http.ResponseWriter, code, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]any{
		"error_code": code,
		"message":    msg,
	}
	_ = json.NewEncoder(w).Encode(resp)
}
