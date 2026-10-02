package handler

import (
	"context"
	"errors"
	"slices"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marketplace/internal/api"
	"marketplace/internal/apierr"
	sqlcdb "marketplace/internal/db/sqlc"
	"marketplace/internal/middleware"
	"marketplace/internal/service"
)

type Handler struct {
	auth     *service.AuthService
	products *service.ProductService
	orders   *service.OrderService
	promos   *service.PromoService
}

func New(
	auth *service.AuthService,
	products *service.ProductService,
	orders *service.OrderService,
	promos *service.PromoService,
) *Handler {
	return &Handler{auth: auth, products: products, orders: orders, promos: promos}
}

func errJSON(err error) api.ErrorResponse {
	var ae *apierr.AppError
	if errors.As(err, &ae) {
		resp := api.ErrorResponse{
			ErrorCode: ae.ErrorCode,
			Message:   ae.Message,
		}
		if ae.Details != nil {
			d := map[string]any(ae.Details)
			resp.Details = &d
		}
		return resp
	}
	return api.ErrorResponse{ErrorCode: "INTERNAL_ERROR", Message: "Внутренняя ошибка сервера"}
}

func appErrCode(err error) string {
	var ae *apierr.AppError
	if errors.As(err, &ae) {
		return ae.ErrorCode
	}
	return ""
}

func toProductResponse(p sqlcdb.Product) api.ProductResponse {
	resp := api.ProductResponse{
		Id:        openapi_types.UUID(p.ID),
		Name:      p.Name,
		Price:     p.Price.InexactFloat64(),
		Stock:     int(p.Stock),
		Category:  p.Category,
		Status:    api.ProductStatus(p.Status),
		CreatedAt: p.CreatedAt.Time,
		UpdatedAt: p.UpdatedAt.Time,
	}
	if p.Description.Valid {
		resp.Description = &p.Description.String
	}
	if p.SellerID.Valid {
		sid := openapi_types.UUID(p.SellerID.Bytes)
		resp.SellerId = &sid
	}
	return resp
}

func toOrderResponse(o sqlcdb.Order, items []sqlcdb.OrderItem) api.OrderResponse {
	resp := api.OrderResponse{
		Id:             openapi_types.UUID(o.ID),
		UserId:         openapi_types.UUID(o.UserID),
		Status:         api.OrderStatus(o.Status),
		TotalAmount:    o.TotalAmount.InexactFloat64(),
		DiscountAmount: o.DiscountAmount.InexactFloat64(),
		CreatedAt:      o.CreatedAt.Time,
		UpdatedAt:      o.UpdatedAt.Time,
		Items:          make([]api.OrderItemResponse, 0, len(items)),
	}
	if o.PromoCodeID.Valid {
		pid := openapi_types.UUID(o.PromoCodeID.Bytes)
		resp.PromoCodeId = &pid
	}
	for _, item := range items {
		resp.Items = append(resp.Items, api.OrderItemResponse{
			Id:           openapi_types.UUID(item.ID),
			ProductId:    openapi_types.UUID(item.ProductID),
			Quantity:     int(item.Quantity),
			PriceAtOrder: item.PriceAtOrder.InexactFloat64(),
		})
	}
	return resp
}

func toAuthResponse(pair service.TokenPair) api.AuthResponse {
	tokenType := "Bearer"
	return api.AuthResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		TokenType:    tokenType,
		ExpiresIn:    pair.ExpiresIn,
	}
}

func userFromCtx(ctx context.Context) (id openapi_types.UUID, role string) {
	uid := middleware.UserIDFromCtx(ctx)
	return openapi_types.UUID(uid), middleware.RoleFromCtx(ctx)
}

func requireRole(role string, allowed ...string) error {
	if slices.Contains(allowed, role) {
		return nil
	}
	return apierr.New(apierr.ErrAccessDenied, "Недостаточно прав для выполнения операции")
}
