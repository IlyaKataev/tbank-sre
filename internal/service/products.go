package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/shopspring/decimal"

	"marketplace/internal/apierr"
	sqlcdb "marketplace/internal/db/sqlc"
)

type ProductService struct {
	q *sqlcdb.Queries
}

func NewProductService(q *sqlcdb.Queries) *ProductService {
	return &ProductService{q: q}
}

type CreateProductInput struct {
	Name        string
	Description *string
	Price       float64
	Stock       int32
	Category    string
	SellerID    uuid.UUID
}

type UpdateProductInput struct {
	Name        string
	Description *string
	Price       float64
	Stock       int32
	Category    string
	Status      string
}

func (s *ProductService) Create(ctx context.Context, in CreateProductInput) (sqlcdb.Product, error) {
	var desc pgtype.Text
	if in.Description != nil {
		desc = pgtype.Text{String: *in.Description, Valid: true}
	}
	sellerID := pgtype.UUID{Bytes: in.SellerID, Valid: true}

	return s.q.CreateProduct(ctx, sqlcdb.CreateProductParams{
		Name:        in.Name,
		Description: desc,
		Price:       decimal.NewFromFloat(in.Price),
		Stock:       in.Stock,
		Category:    in.Category,
		Status:      "ACTIVE",
		SellerID:    sellerID,
	})
}

func (s *ProductService) GetByID(ctx context.Context, id uuid.UUID) (sqlcdb.Product, error) {
	p, err := s.q.GetProductByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcdb.Product{}, apierr.New(apierr.ErrProductNotFound, "Товар не найден")
		}
		return sqlcdb.Product{}, fmt.Errorf("get product: %w", err)
	}
	return p, nil
}

func (s *ProductService) Update(ctx context.Context, id uuid.UUID, in UpdateProductInput, callerID uuid.UUID, callerRole string) (sqlcdb.Product, error) {
	existing, err := s.q.GetProductByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcdb.Product{}, apierr.New(apierr.ErrProductNotFound, "Товар не найден")
		}
		return sqlcdb.Product{}, fmt.Errorf("get product: %w", err)
	}

	if callerRole == "SELLER" {
		sellerID := uuid.UUID(existing.SellerID.Bytes)
		if !existing.SellerID.Valid || sellerID != callerID {
			return sqlcdb.Product{}, apierr.New(apierr.ErrAccessDenied, "Вы не являетесь владельцем товара")
		}
	}

	var desc pgtype.Text
	if in.Description != nil {
		desc = pgtype.Text{String: *in.Description, Valid: true}
	}

	return s.q.UpdateProduct(ctx, sqlcdb.UpdateProductParams{
		ID:          id,
		Name:        in.Name,
		Description: desc,
		Price:       decimal.NewFromFloat(in.Price),
		Stock:       in.Stock,
		Category:    in.Category,
		Status:      in.Status,
	})
}

func (s *ProductService) Delete(ctx context.Context, id uuid.UUID, callerID uuid.UUID, callerRole string) (sqlcdb.Product, error) {
	existing, err := s.q.GetProductByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlcdb.Product{}, apierr.New(apierr.ErrProductNotFound, "Товар не найден")
		}
		return sqlcdb.Product{}, fmt.Errorf("get product: %w", err)
	}

	if callerRole == "SELLER" {
		sellerID := uuid.UUID(existing.SellerID.Bytes)
		if !existing.SellerID.Valid || sellerID != callerID {
			return sqlcdb.Product{}, apierr.New(apierr.ErrAccessDenied, "Вы не являетесь владельцем товара")
		}
	}

	return s.q.ArchiveProduct(ctx, id)
}

type ListProductsInput struct {
	Page     int32
	Size     int32
	Status   *string
	Category *string
}

type PagedProducts struct {
	Items         []sqlcdb.Product
	TotalElements int64
	Page          int32
	Size          int32
}

func (s *ProductService) List(ctx context.Context, in ListProductsInput) (PagedProducts, error) {
	statusParam := pgtype.Text{}
	if in.Status != nil {
		statusParam = pgtype.Text{String: *in.Status, Valid: true}
	}
	categoryParam := pgtype.Text{}
	if in.Category != nil {
		categoryParam = pgtype.Text{String: *in.Category, Valid: true}
	}

	offset := in.Page * in.Size

	items, err := s.q.ListProducts(ctx, sqlcdb.ListProductsParams{
		Status:    statusParam,
		Category:  categoryParam,
		LimitVal:  in.Size,
		OffsetVal: offset,
	})
	if err != nil {
		return PagedProducts{}, fmt.Errorf("list products: %w", err)
	}

	total, err := s.q.CountProducts(ctx, sqlcdb.CountProductsParams{
		Status:   statusParam,
		Category: categoryParam,
	})
	if err != nil {
		return PagedProducts{}, fmt.Errorf("count products: %w", err)
	}

	return PagedProducts{
		Items:         items,
		TotalElements: total,
		Page:          in.Page,
		Size:          in.Size,
	}, nil
}
