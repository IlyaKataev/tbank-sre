package handler

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apierr"
	"marketplace/internal/service"
)

func (h *Handler) CreateOrder(ctx context.Context, req api.CreateOrderRequestObject) (api.CreateOrderResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "USER", "ADMIN"); err != nil {
		return api.CreateOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	v := &validator{}
	v.minInt("items", len(req.Body.Items), 1)
	v.maxInt("items", len(req.Body.Items), 50)
	for i, it := range req.Body.Items {
		v.minInt(fmt.Sprintf("items[%d].quantity", i), it.Quantity, 1)
		v.maxInt(fmt.Sprintf("items[%d].quantity", i), it.Quantity, 999)
	}
	if req.Body.PromoCode != nil {
		v.pattern("promo_code", *req.Body.PromoCode, "pattern: ^[A-Z0-9_]{4,20}$", isPromoCodePattern)
	}
	if err := v.err(); err != nil {
		return api.CreateOrder400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
	}

	items := make([]service.OrderItemInput, 0, len(req.Body.Items))
	for _, it := range req.Body.Items {
		items = append(items, service.OrderItemInput{
			ProductID: uuid.UUID(it.ProductId),
			Quantity:  int32(it.Quantity),
		})
	}

	var promoCode *string
	if req.Body.PromoCode != nil {
		promoCode = req.Body.PromoCode
	}

	result, err := h.orders.Create(ctx, service.CreateOrderInput{
		UserID:    uuid.UUID(callerID),
		Items:     items,
		PromoCode: promoCode,
	})
	if err != nil {
		return mapOrderCreateErr(err), nil
	}
	return api.CreateOrder201JSONResponse(toOrderResponse(result.Order, result.Items)), nil
}

func mapOrderCreateErr(err error) api.CreateOrderResponseObject {
	switch appErrCode(err) {
	case apierr.ErrOrderLimitExceeded:
		return api.CreateOrder429JSONResponse{OrderLimitExceededJSONResponse: api.OrderLimitExceededJSONResponse(errJSON(err))}
	case apierr.ErrOrderHasActive, apierr.ErrProductInactive, apierr.ErrInsufficientStock:
		return api.CreateOrder409JSONResponse(errJSON(err))
	case apierr.ErrProductNotFound:
		return api.CreateOrder404JSONResponse(errJSON(err))
	case apierr.ErrPromoCodeInvalid:
		return api.CreateOrder422JSONResponse(errJSON(err))
	case apierr.ErrPromoCodeMinAmount:
		return api.CreateOrder422JSONResponse(errJSON(err))
	case apierr.ErrValidation:
		return api.CreateOrder400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}
	case apierr.ErrAccessDenied:
		return api.CreateOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}
	}
	return nil
}

func (h *Handler) GetOrder(ctx context.Context, req api.GetOrderRequestObject) (api.GetOrderResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "USER", "ADMIN"); err != nil {
		return api.GetOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	result, err := h.orders.Get(ctx, uuid.UUID(req.Id), uuid.UUID(callerID), role)
	if err != nil {
		switch appErrCode(err) {
		case apierr.ErrOrderNotFound:
			return api.GetOrder404JSONResponse{OrderNotFoundJSONResponse: api.OrderNotFoundJSONResponse(errJSON(err))}, nil
		case apierr.ErrOrderOwnershipViolation, apierr.ErrAccessDenied:
			return api.GetOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.GetOrder200JSONResponse(toOrderResponse(result.Order, result.Items)), nil
}

func (h *Handler) UpdateOrder(ctx context.Context, req api.UpdateOrderRequestObject) (api.UpdateOrderResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "USER", "ADMIN"); err != nil {
		return api.UpdateOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	v := &validator{}
	v.minInt("items", len(req.Body.Items), 1)
	v.maxInt("items", len(req.Body.Items), 50)
	for i, it := range req.Body.Items {
		v.minInt(fmt.Sprintf("items[%d].quantity", i), it.Quantity, 1)
		v.maxInt(fmt.Sprintf("items[%d].quantity", i), it.Quantity, 999)
	}
	if err := v.err(); err != nil {
		return api.UpdateOrder400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
	}

	items := make([]service.OrderItemInput, 0, len(req.Body.Items))
	for _, it := range req.Body.Items {
		items = append(items, service.OrderItemInput{
			ProductID: uuid.UUID(it.ProductId),
			Quantity:  int32(it.Quantity),
		})
	}

	result, err := h.orders.Update(ctx, service.UpdateOrderInput{
		OrderID:    uuid.UUID(req.Id),
		CallerID:   uuid.UUID(callerID),
		CallerRole: role,
		Items:      items,
	})
	if err != nil {
		switch appErrCode(err) {
		case apierr.ErrOrderNotFound:
			return api.UpdateOrder404JSONResponse{OrderNotFoundJSONResponse: api.OrderNotFoundJSONResponse(errJSON(err))}, nil
		case apierr.ErrOrderOwnershipViolation, apierr.ErrAccessDenied:
			return api.UpdateOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
		case apierr.ErrInvalidStateTransition, apierr.ErrProductInactive, apierr.ErrInsufficientStock:
			return api.UpdateOrder409JSONResponse(errJSON(err)), nil
		case apierr.ErrOrderLimitExceeded:
			return api.UpdateOrder429JSONResponse{OrderLimitExceededJSONResponse: api.OrderLimitExceededJSONResponse(errJSON(err))}, nil
		case apierr.ErrProductNotFound:
			return api.UpdateOrder404JSONResponse{OrderNotFoundJSONResponse: api.OrderNotFoundJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.UpdateOrder200JSONResponse(toOrderResponse(result.Order, result.Items)), nil
}

func (h *Handler) CancelOrder(ctx context.Context, req api.CancelOrderRequestObject) (api.CancelOrderResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "USER", "ADMIN"); err != nil {
		return api.CancelOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	result, err := h.orders.Cancel(ctx, uuid.UUID(req.Id), uuid.UUID(callerID), role)
	if err != nil {
		switch appErrCode(err) {
		case apierr.ErrOrderNotFound:
			return api.CancelOrder404JSONResponse{OrderNotFoundJSONResponse: api.OrderNotFoundJSONResponse(errJSON(err))}, nil
		case apierr.ErrOrderOwnershipViolation, apierr.ErrAccessDenied:
			return api.CancelOrder403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
		case apierr.ErrInvalidStateTransition:
			return api.CancelOrder409JSONResponse(errJSON(err)), nil
		}
		return nil, err
	}
	return api.CancelOrder200JSONResponse(toOrderResponse(result.Order, result.Items)), nil
}
