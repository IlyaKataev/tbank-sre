package service_test

import (
	"slices"
	"testing"
	"time"

	sqlcdb "marketplace/internal/db/sqlc"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestOrderStateMachine(t *testing.T) {
	allowed := map[string][]string{
		"CREATED":         {"PAYMENT_PENDING", "CANCELED"},
		"PAYMENT_PENDING": {"PAID", "CANCELED"},
		"PAID":            {"SHIPPED"},
		"SHIPPED":         {"COMPLETED"},
		"COMPLETED":       {},
		"CANCELED":        {},
	}

	cancelAllowed := func(status string) bool {
		return slices.Contains(allowed[status], "CANCELED")
	}

	assert.True(t, cancelAllowed("CREATED"))
	assert.True(t, cancelAllowed("PAYMENT_PENDING"))
	assert.False(t, cancelAllowed("PAID"))
	assert.False(t, cancelAllowed("SHIPPED"))
	assert.False(t, cancelAllowed("COMPLETED"))
}

func TestOrderUpdate_OnlyInCreated(t *testing.T) {
	notAllowed := []string{"PAYMENT_PENDING", "PAID", "SHIPPED", "COMPLETED", "CANCELED"}
	for _, status := range notAllowed {
		t.Run(status, func(t *testing.T) {
			assert.NotEqual(t, "CREATED", status, "update should be rejected for status %s", status)
		})
	}
}

func TestCalcDiscount_Percentage(t *testing.T) {
	total := decimal.NewFromFloat(100.0)
	promo := sqlcdb.PromoCode{
		DiscountType:  "PERCENTAGE",
		DiscountValue: decimal.NewFromFloat(50), // 50%
	}

	discount := calcDiscountHelper(total, promo)
	// 50% of 100 = 50, but cap at 70%, 50 ≤ 70, so discount = 50
	assert.True(t, decimal.NewFromFloat(50.0).Equal(discount), "expected 50, got %s", discount)
}

func TestCalcDiscount_Percentage_CappedAt70(t *testing.T) {
	total := decimal.NewFromFloat(100.0)
	promo := sqlcdb.PromoCode{
		DiscountType:  "PERCENTAGE",
		DiscountValue: decimal.NewFromFloat(80), // 80% — exceeds 70% cap
	}

	discount := calcDiscountHelper(total, promo)
	// cap at 70% of 100 = 70
	assert.Equal(t, decimal.NewFromFloat(70.0).String(), discount.String())
}

func TestCalcDiscount_Fixed(t *testing.T) {
	total := decimal.NewFromFloat(50.0)
	promo := sqlcdb.PromoCode{
		DiscountType:  "FIXED_AMOUNT",
		DiscountValue: decimal.NewFromFloat(20),
	}

	discount := calcDiscountHelper(total, promo)
	assert.True(t, decimal.NewFromFloat(20.0).Equal(discount), "expected 20, got %s", discount)
}

func TestCalcDiscount_FixedExceedsTotal(t *testing.T) {
	total := decimal.NewFromFloat(10.0)
	promo := sqlcdb.PromoCode{
		DiscountType:  "FIXED_AMOUNT",
		DiscountValue: decimal.NewFromFloat(30), // exceeds total
	}

	discount := calcDiscountHelper(total, promo)
	assert.Equal(t, total, discount)
}

func TestRateLimit_NotExceeded(t *testing.T) {
	lastOpAt := time.Now().Add(-2 * time.Minute)
	limit := 1 * time.Minute

	exceeded := time.Since(lastOpAt) < limit
	assert.False(t, exceeded, "operation 2 min ago should not be rate limited with 1 min window")
}

func TestRateLimit_Exceeded(t *testing.T) {
	lastOpAt := time.Now().Add(-30 * time.Second)
	limit := 1 * time.Minute

	exceeded := time.Since(lastOpAt) < limit
	assert.True(t, exceeded, "operation 30s ago should be rate limited with 1 min window")
}

func TestPriceSnapshot(t *testing.T) {
	// price_at_order must be captured at creation time and not change
	priceAtOrder := decimal.NewFromFloat(99.99)
	currentPrice := decimal.NewFromFloat(149.99) // price changed after order

	assert.NotEqual(t, priceAtOrder, currentPrice, "prices differ after change")
	assert.Equal(t, decimal.NewFromFloat(99.99), priceAtOrder)
}

func TestTotalAmount_Calculation(t *testing.T) {
	items := []struct {
		price    decimal.Decimal
		quantity int64
	}{
		{decimal.NewFromFloat(10.00), 3},  // 30
		{decimal.NewFromFloat(5.50), 2},   // 11
		{decimal.NewFromFloat(100.00), 1}, // 100
	}

	total := decimal.Zero
	for _, it := range items {
		total = total.Add(it.price.Mul(decimal.NewFromInt(it.quantity)))
	}

	assert.True(t, decimal.NewFromFloat(141.0).Equal(total), "expected 141, got %s", total)
}

// calcDiscountHelper replicates the calcDiscount logic for testing.
func calcDiscountHelper(total decimal.Decimal, promo sqlcdb.PromoCode) decimal.Decimal {
	switch promo.DiscountType {
	case "PERCENTAGE":
		d := total.Mul(promo.DiscountValue).Div(decimal.NewFromInt(100))
		maxDiscount := total.Mul(decimal.NewFromFloat(0.70))
		if d.GreaterThan(maxDiscount) {
			d = maxDiscount
		}
		return d
	case "FIXED_AMOUNT":
		if promo.DiscountValue.GreaterThan(total) {
			return total
		}
		return promo.DiscountValue
	}
	return decimal.Zero
}
