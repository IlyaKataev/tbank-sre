package handler

import (
	"context"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apierr"
	"marketplace/internal/service"
)

func (h *Handler) ListProducts(ctx context.Context, req api.ListProductsRequestObject) (api.ListProductsResponseObject, error) {
	page := int32(0)
	size := int32(20)
	if req.Params.Page != nil {
		page = int32(*req.Params.Page)
	}
	if req.Params.Size != nil {
		size = int32(*req.Params.Size)
	}
	var statusFilter *string
	if req.Params.Status != nil {
		s := string(*req.Params.Status)
		statusFilter = &s
	}

	result, err := h.products.List(ctx, service.ListProductsInput{
		Page:     page,
		Size:     size,
		Status:   statusFilter,
		Category: req.Params.Category,
	})
	if err != nil {
		return nil, err
	}

	items := make([]api.ProductResponse, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, toProductResponse(result.Items[i]))
	}

	return api.ListProducts200JSONResponse(api.PagedProducts{
		Items:         items,
		TotalElements: result.TotalElements,
		Page:          int(result.Page),
		Size:          int(result.Size),
	}), nil
}

func (h *Handler) CreateProduct(ctx context.Context, req api.CreateProductRequestObject) (api.CreateProductResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "SELLER", "ADMIN"); err != nil {
		return api.CreateProduct403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	v := &validator{}
	v.minLen("name", req.Body.Name, 1)
	v.maxLen("name", req.Body.Name, 255)
	v.minFloat("price", req.Body.Price, 0.01)
	v.minInt("stock", int(req.Body.Stock), 0)
	v.minLen("category", req.Body.Category, 1)
	v.maxLen("category", req.Body.Category, 100)
	if req.Body.Description != nil {
		v.maxLen("description", *req.Body.Description, 4000)
	}
	if err := v.err(); err != nil {
		return api.CreateProduct400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
	}

	product, err := h.products.Create(ctx, service.CreateProductInput{
		Name:        req.Body.Name,
		Description: req.Body.Description,
		Price:       req.Body.Price,
		Stock:       int32(req.Body.Stock),
		Category:    req.Body.Category,
		SellerID:    uuid.UUID(callerID),
	})
	if err != nil {
		return nil, err
	}
	return api.CreateProduct201JSONResponse(toProductResponse(product)), nil
}

func (h *Handler) GetProduct(ctx context.Context, req api.GetProductRequestObject) (api.GetProductResponseObject, error) {
	p, err := h.products.GetByID(ctx, uuid.UUID(req.Id))
	if err != nil {
		if appErrCode(err) == apierr.ErrProductNotFound {
			return api.GetProduct404JSONResponse{ProductNotFoundJSONResponse: api.ProductNotFoundJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.GetProduct200JSONResponse(toProductResponse(p)), nil
}

func (h *Handler) UpdateProduct(ctx context.Context, req api.UpdateProductRequestObject) (api.UpdateProductResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "SELLER", "ADMIN"); err != nil {
		return api.UpdateProduct403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	v := &validator{}
	v.minLen("name", req.Body.Name, 1)
	v.maxLen("name", req.Body.Name, 255)
	v.minFloat("price", req.Body.Price, 0.01)
	v.minInt("stock", int(req.Body.Stock), 0)
	v.minLen("category", req.Body.Category, 1)
	v.maxLen("category", req.Body.Category, 100)
	if req.Body.Description != nil {
		v.maxLen("description", *req.Body.Description, 4000)
	}
	if err := v.err(); err != nil {
		return api.UpdateProduct400JSONResponse{ValidationErrorJSONResponse: api.ValidationErrorJSONResponse(errJSON(err))}, nil
	}

	p, err := h.products.Update(ctx, uuid.UUID(req.Id), service.UpdateProductInput{
		Name:        req.Body.Name,
		Description: req.Body.Description,
		Price:       req.Body.Price,
		Stock:       int32(req.Body.Stock),
		Category:    req.Body.Category,
		Status:      string(req.Body.Status),
	}, uuid.UUID(callerID), role)
	if err != nil {
		switch appErrCode(err) {
		case apierr.ErrProductNotFound:
			return api.UpdateProduct404JSONResponse{ProductNotFoundJSONResponse: api.ProductNotFoundJSONResponse(errJSON(err))}, nil
		case apierr.ErrAccessDenied:
			return api.UpdateProduct403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.UpdateProduct200JSONResponse(toProductResponse(p)), nil
}

func (h *Handler) DeleteProduct(ctx context.Context, req api.DeleteProductRequestObject) (api.DeleteProductResponseObject, error) {
	callerID, role := userFromCtx(ctx)
	if err := requireRole(role, "SELLER", "ADMIN"); err != nil {
		return api.DeleteProduct403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
	}

	p, err := h.products.Delete(ctx, uuid.UUID(req.Id), uuid.UUID(callerID), role)
	if err != nil {
		switch appErrCode(err) {
		case apierr.ErrProductNotFound:
			return api.DeleteProduct404JSONResponse{ProductNotFoundJSONResponse: api.ProductNotFoundJSONResponse(errJSON(err))}, nil
		case apierr.ErrAccessDenied:
			return api.DeleteProduct403JSONResponse{ForbiddenJSONResponse: api.ForbiddenJSONResponse(errJSON(err))}, nil
		}
		return nil, err
	}
	return api.DeleteProduct200JSONResponse(toProductResponse(p)), nil
}
