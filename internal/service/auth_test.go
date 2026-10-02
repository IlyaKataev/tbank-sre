package service_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"marketplace/internal/apierr"
	"marketplace/internal/middleware"
)

func TestBcrypt_HashAndVerify(t *testing.T) {
	password := "secret123"
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)

	err = bcrypt.CompareHashAndPassword(hash, []byte(password))
	assert.NoError(t, err)

	err = bcrypt.CompareHashAndPassword(hash, []byte("wrong"))
	assert.Error(t, err)
}

func TestHashToken_Deterministic(t *testing.T) {
	token := "test-token-123"
	h1 := hashTokenHelper(token)
	h2 := hashTokenHelper(token)
	assert.Equal(t, h1, h2, "same token should always produce same hash")
	assert.Len(t, h1, 64, "SHA-256 hex should be 64 chars")
}

func TestJWT_SignAndValidate(t *testing.T) {
	secret := "test-secret"
	userID := uuid.New()
	role := "USER"

	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
		Role: role,
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	parsed := &middleware.Claims{}
	tok, err := jwt.ParseWithClaims(token, parsed, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	require.NoError(t, err)
	assert.True(t, tok.Valid)
	assert.Equal(t, userID.String(), parsed.Subject)
	assert.Equal(t, role, parsed.Role)
}

func TestJWT_ExpiredToken(t *testing.T) {
	secret := "test-secret"
	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	require.NoError(t, err)

	parsed := &middleware.Claims{}
	_, err = jwt.ParseWithClaims(token, parsed, func(t *jwt.Token) (any, error) {
		return []byte(secret), nil
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, jwt.ErrTokenExpired)
}

func TestJWT_WrongSecret(t *testing.T) {
	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   uuid.New().String(),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("correct-secret"))
	require.NoError(t, err)

	_, err = jwt.ParseWithClaims(token, &middleware.Claims{}, func(t *jwt.Token) (any, error) {
		return []byte("wrong-secret"), nil
	})
	require.Error(t, err, "wrong secret should fail validation")
}

func TestAuthErrors_Codes(t *testing.T) {
	tests := []struct {
		code   string
		status int
	}{
		{apierr.ErrInvalidCredentials, 401},
		{apierr.ErrUserAlreadyExists, 409},
		{apierr.ErrRefreshTokenInvalid, 401},
		{apierr.ErrTokenExpired, 401},
		{apierr.ErrTokenInvalid, 401},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.status, apierr.HTTPStatusCode(tt.code))
		})
	}
}

func hashTokenHelper(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
