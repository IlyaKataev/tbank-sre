package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"marketplace/internal/apierr"
	sqlcdb "marketplace/internal/db/sqlc"
	"marketplace/internal/middleware"
)

type AuthService struct {
	q          *sqlcdb.Queries
	jwtSecret  string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // seconds
}

func NewAuthService(q *sqlcdb.Queries, secret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{q: q, jwtSecret: secret, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (s *AuthService) Register(ctx context.Context, email, password, role string) (TokenPair, error) {
	if role == "" {
		role = "USER"
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return TokenPair{}, fmt.Errorf("hash password: %w", err)
	}

	user, err := s.q.CreateUser(ctx, sqlcdb.CreateUserParams{
		Email:        email,
		PasswordHash: string(hash),
		Role:         role,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return TokenPair{}, apierr.New(apierr.ErrUserAlreadyExists, "Email уже занят")
		}
		return TokenPair{}, fmt.Errorf("create user: %w", err)
	}

	return s.issueTokens(ctx, user.ID, user.Role)
}

func (s *AuthService) Login(ctx context.Context, email, password string) (TokenPair, error) {
	user, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenPair{}, apierr.New(apierr.ErrInvalidCredentials, "Неверный email или пароль")
		}
		return TokenPair{}, fmt.Errorf("get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return TokenPair{}, apierr.New(apierr.ErrInvalidCredentials, "Неверный email или пароль")
	}

	return s.issueTokens(ctx, user.ID, user.Role)
}

func (s *AuthService) RefreshTokens(ctx context.Context, refreshToken string) (TokenPair, error) {
	h := hashToken(refreshToken)
	row, err := s.q.GetRefreshToken(ctx, h)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TokenPair{}, apierr.New(apierr.ErrRefreshTokenInvalid, "Refresh token недействителен или истёк")
		}
		return TokenPair{}, fmt.Errorf("get refresh token: %w", err)
	}

	_ = s.q.DeleteRefreshToken(ctx, h)

	userID := row.UserID
	return s.issueTokens(ctx, userID, row.Role)
}

func (s *AuthService) issueTokens(ctx context.Context, userID uuid.UUID, role string) (TokenPair, error) {
	now := time.Now()

	claims := middleware.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
		Role: role,
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.jwtSecret))
	if err != nil {
		return TokenPair{}, fmt.Errorf("sign access token: %w", err)
	}

	refreshToken := uuid.New().String() + "-" + uuid.New().String()
	h := hashToken(refreshToken)

	expiresAt := now.Add(s.refreshTTL)
	_, err = s.q.CreateRefreshToken(ctx, sqlcdb.CreateRefreshTokenParams{
		UserID:    userID,
		TokenHash: h,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		return TokenPair{}, fmt.Errorf("store refresh token: %w", err)
	}

	return TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int(s.accessTTL.Seconds()),
	}, nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}
