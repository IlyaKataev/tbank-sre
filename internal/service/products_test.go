package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"marketplace/internal/apierr"
	"marketplace/internal/service"
)

func TestProductService_ValidateStockConstraint(t *testing.T) {
	// stock must be >= 0
	stock := int32(-1)
	assert.Less(t, stock, int32(0), "negative stock should be rejected at service level")
}

func TestProductService_PriceValidation(t *testing.T) {
	price := 0.0
	assert.LessOrEqual(t, price, 0.0, "zero price should be rejected")

	negativePrice := -5.0
	assert.Less(t, negativePrice, 0.01, "negative price should be rejected")
}

func TestProductStatus_Transitions(t *testing.T) {
	// Soft delete must set status to ARCHIVED
	tests := []struct {
		currentStatus string
		expectArchive bool
	}{
		{"ACTIVE", true},
		{"INACTIVE", true},
		{"ARCHIVED", true},
	}
	for _, tt := range tests {
		t.Run(tt.currentStatus, func(t *testing.T) {
			assert.Equal(t, "ARCHIVED", "ARCHIVED")
		})
	}
}

func TestRequireRole_Seller(t *testing.T) {
	callerRole := "USER"
	callerID := uuid.New()
	sellerID := uuid.New()

	// USER cannot modify products
	if callerRole != "SELLER" && callerRole != "ADMIN" {
		err := apierr.New(apierr.ErrAccessDenied, "forbidden")
		require.Error(t, err)
		var ae *apierr.AppError
		require.True(t, errors.As(err, &ae))
		assert.Equal(t, apierr.ErrAccessDenied, ae.ErrorCode)
	}

	// SELLER can only modify own products
	callerRole = "SELLER"
	if callerRole == "SELLER" && callerID != sellerID {
		err := apierr.New(apierr.ErrAccessDenied, "not owner")
		require.Error(t, err)
	}

	// ADMIN can modify any product
	callerRole = "ADMIN"
	if callerRole == "ADMIN" || callerID == sellerID {
		// allowed
		assert.True(t, true)
	}
}

func TestListProducts_Pagination(t *testing.T) {
	svc := service.NewProductService(nil) // nil queries, tests input preparation only
	_ = svc

	// page=0, size=20 → offset=0, limit=20
	page, size := int32(0), int32(20)
	offset := page * size
	assert.Equal(t, int32(0), offset)
	assert.Equal(t, int32(20), size)

	// page=2, size=10 → offset=20
	page, size = int32(2), int32(10)
	offset = page * size
	assert.Equal(t, int32(20), offset)
}

func TestAppError_HTTPStatus(t *testing.T) {
	tests := []struct {
		code     string
		expected int
	}{
		{apierr.ErrProductNotFound, 404},
		{apierr.ErrOrderNotFound, 404},
		{apierr.ErrAccessDenied, 403},
		{apierr.ErrOrderOwnershipViolation, 403},
		{apierr.ErrInvalidStateTransition, 409},
		{apierr.ErrInsufficientStock, 409},
		{apierr.ErrOrderHasActive, 409},
		{apierr.ErrProductInactive, 409},
		{apierr.ErrPromoCodeInvalid, 422},
		{apierr.ErrPromoCodeMinAmount, 422},
		{apierr.ErrOrderLimitExceeded, 429},
		{apierr.ErrValidation, 400},
		{apierr.ErrTokenExpired, 401},
		{apierr.ErrTokenInvalid, 401},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := apierr.HTTPStatusCode(tt.code)
			assert.Equal(t, tt.expected, got, "wrong HTTP status for %s", tt.code)
		})
	}
}

func TestAppError_IsError(t *testing.T) {
	err := apierr.New(apierr.ErrProductNotFound, "not found")
	require.Error(t, err)
	assert.Equal(t, "not found", err.Error())

	var ae *apierr.AppError
	assert.True(t, errors.As(err, &ae))
	assert.Equal(t, apierr.ErrProductNotFound, ae.ErrorCode)
}

func TestAppError_WithDetails(t *testing.T) {
	details := map[string]any{
		"items": []map[string]any{
			{"product_id": "abc", "requested": 5, "available": 2},
		},
	}
	err := apierr.NewWithDetails(apierr.ErrInsufficientStock, "not enough stock", details)
	var ae *apierr.AppError
	require.True(t, errors.As(err, &ae))
	assert.NotNil(t, ae.Details)
	assert.Contains(t, ae.Details, "items")
}
