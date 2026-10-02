package apierr

import "net/http"

const (
	ErrProductNotFound         = "PRODUCT_NOT_FOUND"
	ErrProductInactive         = "PRODUCT_INACTIVE"
	ErrOrderNotFound           = "ORDER_NOT_FOUND"
	ErrOrderLimitExceeded      = "ORDER_LIMIT_EXCEEDED"
	ErrOrderHasActive          = "ORDER_HAS_ACTIVE"
	ErrInvalidStateTransition  = "INVALID_STATE_TRANSITION"
	ErrInsufficientStock       = "INSUFFICIENT_STOCK"
	ErrPromoCodeInvalid        = "PROMO_CODE_INVALID"
	ErrPromoCodeMinAmount      = "PROMO_CODE_MIN_AMOUNT"
	ErrOrderOwnershipViolation = "ORDER_OWNERSHIP_VIOLATION"
	ErrValidation              = "VALIDATION_ERROR"
	ErrTokenExpired            = "TOKEN_EXPIRED"
	ErrTokenInvalid            = "TOKEN_INVALID"
	ErrRefreshTokenInvalid     = "REFRESH_TOKEN_INVALID"
	ErrAccessDenied            = "ACCESS_DENIED"
	ErrUserAlreadyExists       = "USER_ALREADY_EXISTS"
	ErrInvalidCredentials      = "INVALID_CREDENTIALS"
)

type AppError struct {
	ErrorCode  string
	Message    string
	HTTPStatus int
	Details    map[string]any
}

func (e *AppError) Error() string { return e.Message }

func HTTPStatusCode(code string) int {
	switch code {
	case ErrValidation:
		return http.StatusBadRequest
	case ErrTokenExpired, ErrTokenInvalid, ErrRefreshTokenInvalid, ErrInvalidCredentials:
		return http.StatusUnauthorized
	case ErrAccessDenied, ErrOrderOwnershipViolation:
		return http.StatusForbidden
	case ErrProductNotFound, ErrOrderNotFound:
		return http.StatusNotFound
	case ErrProductInactive, ErrOrderHasActive, ErrInvalidStateTransition, ErrInsufficientStock, ErrUserAlreadyExists:
		return http.StatusConflict
	case ErrPromoCodeInvalid, ErrPromoCodeMinAmount:
		return http.StatusUnprocessableEntity
	case ErrOrderLimitExceeded:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func New(code, message string) *AppError {
	return &AppError{
		ErrorCode:  code,
		Message:    message,
		HTTPStatus: HTTPStatusCode(code),
	}
}

func NewWithDetails(code, message string, details map[string]any) *AppError {
	return &AppError{
		ErrorCode:  code,
		Message:    message,
		HTTPStatus: HTTPStatusCode(code),
		Details:    details,
	}
}
