package handler

import (
	"context"
	"time"

	"marketplace/internal/api"
	"marketplace/internal/apierr"
	"marketplace/internal/service"
)

func (h *Handler) CreatePromoCode(ctx context.Context, req api.CreatePromoCodeRequestObject) (api.CreatePromoCodeResponseObject, error) {
	_, role := userFromCtx(ctx)
	if err := requireRole(role, "SELLER", "ADMIN"); err != nil {
		return api.CreatePromoCode403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	validFrom, err := time.Parse(time.RFC3339, req.Body.ValidFrom.Format(time.RFC3339))
	if err != nil {
		return api.CreatePromoCode400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(
			api.ErrorResponse{ErrorCode: "VALIDATION_ERROR", Message: "Неверный формат valid_from"},
		)}, nil
	}
	validUntil, err := time.Parse(time.RFC3339, req.Body.ValidUntil.Format(time.RFC3339))
	if err != nil {
		return api.CreatePromoCode400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(
			api.ErrorResponse{ErrorCode: "VALIDATION_ERROR", Message: "Неверный формат valid_until"},
		)}, nil
	}

	var minAmount *float64
	if req.Body.MinOrderAmount != nil {
		minAmount = req.Body.MinOrderAmount
	}

	promo, err := h.promos.Create(ctx, service.CreatePromoInput{
		Code:           req.Body.Code,
		DiscountType:   string(req.Body.DiscountType),
		DiscountValue:  req.Body.DiscountValue,
		MinOrderAmount: minAmount,
		MaxUses:        int32(req.Body.MaxUses),
		ValidFrom:      validFrom,
		ValidUntil:     validUntil,
	})
	if err != nil {
		if appErrCode(err) == "PROMO_CODE_DUPLICATE" {
			errResp := errJSON(err)
			errResp.ErrorCode = apierr.ErrPromoCodeInvalid
			return api.CreatePromoCode409JSONResponse(errResp), nil
		}
		return nil, err
	}

	resp := api.PromoCodeResponse{
		Id:            promo.ID,
		Code:          promo.Code,
		DiscountType:  api.DiscountType(promo.DiscountType),
		DiscountValue: promo.DiscountValue.InexactFloat64(),
		MaxUses:       int(promo.MaxUses),
		CurrentUses:   int(promo.CurrentUses),
		ValidFrom:     promo.ValidFrom.Time,
		ValidUntil:    promo.ValidUntil.Time,
		Active:        promo.Active,
	}
	if !promo.MinOrderAmount.IsZero() {
		v := promo.MinOrderAmount.InexactFloat64()
		resp.MinOrderAmount = &v
	}

	return api.CreatePromoCode201JSONResponse(resp), nil
}
