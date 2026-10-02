package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"marketplace/internal/apierr"
	sqlcdb "marketplace/internal/db/sqlc"
)

type PromoService struct {
	q *sqlcdb.Queries
}

func NewPromoService(q *sqlcdb.Queries) *PromoService {
	return &PromoService{q: q}
}

type CreatePromoInput struct {
	Code           string
	DiscountType   string
	DiscountValue  float64
	MinOrderAmount *float64
	MaxUses        int32
	ValidFrom      time.Time
	ValidUntil     time.Time
}

func (s *PromoService) Create(ctx context.Context, in CreatePromoInput) (sqlcdb.PromoCode, error) {
	minAmount := decimal.Zero
	if in.MinOrderAmount != nil {
		minAmount = decimal.NewFromFloat(*in.MinOrderAmount)
	}

	promo, err := s.q.CreatePromoCode(ctx, sqlcdb.CreatePromoCodeParams{
		Code:           in.Code,
		DiscountType:   in.DiscountType,
		DiscountValue:  decimal.NewFromFloat(in.DiscountValue),
		MinOrderAmount: minAmount,
		MaxUses:        in.MaxUses,
		ValidFrom:      pgtype.Timestamptz{Time: in.ValidFrom, Valid: true},
		ValidUntil:     pgtype.Timestamptz{Time: in.ValidUntil, Valid: true},
	})
	if err != nil {
		if isUniqueViolation(err) {
			return sqlcdb.PromoCode{}, apierr.New("PROMO_CODE_DUPLICATE", "Промокод с таким кодом уже существует")
		}
		return sqlcdb.PromoCode{}, fmt.Errorf("create promo: %w", err)
	}
	return promo, nil
}
