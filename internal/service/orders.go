package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"marketplace/internal/apierr"
	sqlcdb "marketplace/internal/db/sqlc"
)

type OrderService struct {
	q                *sqlcdb.Queries
	pool             *pgxpool.Pool
	rateLimitMinutes int
}

func NewOrderService(q *sqlcdb.Queries, pool *pgxpool.Pool, rateLimitMinutes int) *OrderService {
	return &OrderService{q: q, pool: pool, rateLimitMinutes: rateLimitMinutes}
}

type OrderItemInput struct {
	ProductID uuid.UUID
	Quantity  int32
}

type CreateOrderInput struct {
	UserID    uuid.UUID
	Items     []OrderItemInput
	PromoCode *string
}

type OrderWithItems struct {
	Order sqlcdb.Order
	Items []sqlcdb.OrderItem
}

func (s *OrderService) Create(ctx context.Context, in CreateOrderInput) (OrderWithItems, error) {
	if err := s.checkRateLimit(ctx, in.UserID, "CREATE_ORDER"); err != nil {
		return OrderWithItems{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.q.WithTx(tx)

	_, err = qtx.GetActiveOrderByUserID(ctx, in.UserID)
	if err == nil {
		return OrderWithItems{}, apierr.New(apierr.ErrOrderHasActive, "У пользователя уже есть активный заказ")
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return OrderWithItems{}, fmt.Errorf("check active order: %w", err)
	}

	type productEntry struct {
		product  sqlcdb.Product
		quantity int32
	}
	var entries []productEntry
	var stockErrors []map[string]any

	for _, item := range in.Items {
		p, err := qtx.GetProductByIDForUpdate(ctx, item.ProductID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return OrderWithItems{}, apierr.New(apierr.ErrProductNotFound, fmt.Sprintf("Товар %s не найден", item.ProductID))
			}
			return OrderWithItems{}, fmt.Errorf("get product: %w", err)
		}
		if p.Status != "ACTIVE" {
			return OrderWithItems{}, apierr.New(apierr.ErrProductInactive, fmt.Sprintf("Товар %s неактивен", item.ProductID))
		}
		if p.Stock < item.Quantity {
			stockErrors = append(stockErrors, map[string]any{
				"product_id": item.ProductID.String(),
				"requested":  item.Quantity,
				"available":  p.Stock,
			})
		}
		entries = append(entries, productEntry{product: p, quantity: item.Quantity})
	}

	if len(stockErrors) > 0 {
		return OrderWithItems{}, apierr.NewWithDetails(apierr.ErrInsufficientStock,
			"Недостаточно товара на складе",
			map[string]any{"items": stockErrors})
	}

	total := decimal.Zero
	for i := range entries {
		if err := qtx.DecrementStock(ctx, sqlcdb.DecrementStockParams{
			ID:    entries[i].product.ID,
			Stock: entries[i].quantity,
		}); err != nil {
			return OrderWithItems{}, fmt.Errorf("decrement stock: %w", err)
		}
		total = total.Add(entries[i].product.Price.Mul(decimal.NewFromInt(int64(entries[i].quantity))))
	}

	discount := decimal.Zero
	var promoCodeID pgtype.UUID
	if in.PromoCode != nil {
		promo, err := qtx.GetPromoCodeByCode(ctx, *in.PromoCode)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return OrderWithItems{}, apierr.New(apierr.ErrPromoCodeInvalid, "Промокод не найден")
			}
			return OrderWithItems{}, fmt.Errorf("get promo: %w", err)
		}

		now := time.Now()
		if !promo.Active || promo.CurrentUses >= promo.MaxUses ||
			now.Before(promo.ValidFrom.Time) || now.After(promo.ValidUntil.Time) {
			return OrderWithItems{}, apierr.New(apierr.ErrPromoCodeInvalid, "Промокод недействителен")
		}
		if total.LessThan(promo.MinOrderAmount) {
			return OrderWithItems{}, apierr.NewWithDetails(apierr.ErrPromoCodeMinAmount,
				"Сумма заказа ниже минимальной для промокода",
				map[string]any{"min_order_amount": promo.MinOrderAmount.InexactFloat64()})
		}

		discount = calcDiscount(total, promo)
		total = total.Sub(discount)

		if err := qtx.IncrementPromoUses(ctx, promo.ID); err != nil {
			return OrderWithItems{}, fmt.Errorf("increment promo uses: %w", err)
		}
		promoCodeID = pgtype.UUID{Bytes: promo.ID, Valid: true}
	}

	order, err := qtx.CreateOrder(ctx, sqlcdb.CreateOrderParams{
		UserID:         in.UserID,
		PromoCodeID:    promoCodeID,
		TotalAmount:    total,
		DiscountAmount: discount,
	})
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("create order: %w", err)
	}

	var orderItems []sqlcdb.OrderItem
	for i := range entries {
		oi, err := qtx.CreateOrderItem(ctx, sqlcdb.CreateOrderItemParams{
			OrderID:      order.ID,
			ProductID:    entries[i].product.ID,
			Quantity:     entries[i].quantity,
			PriceAtOrder: entries[i].product.Price,
		})
		if err != nil {
			return OrderWithItems{}, fmt.Errorf("create order item: %w", err)
		}
		orderItems = append(orderItems, oi)
	}

	if _, err := qtx.CreateUserOperation(ctx, sqlcdb.CreateUserOperationParams{
		UserID:        in.UserID,
		OperationType: "CREATE_ORDER",
	}); err != nil {
		return OrderWithItems{}, fmt.Errorf("record operation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return OrderWithItems{}, fmt.Errorf("commit: %w", err)
	}

	return OrderWithItems{Order: order, Items: orderItems}, nil
}

func (s *OrderService) Get(ctx context.Context, id uuid.UUID, callerID uuid.UUID, callerRole string) (OrderWithItems, error) {
	order, err := s.q.GetOrderByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrderWithItems{}, apierr.New(apierr.ErrOrderNotFound, "Заказ не найден")
		}
		return OrderWithItems{}, fmt.Errorf("get order: %w", err)
	}

	if callerRole != "ADMIN" && order.UserID != callerID {
		return OrderWithItems{}, apierr.New(apierr.ErrOrderOwnershipViolation, "Заказ принадлежит другому пользователю")
	}

	items, err := s.q.GetOrderItems(ctx, id)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("get order items: %w", err)
	}

	return OrderWithItems{Order: order, Items: items}, nil
}

type UpdateOrderInput struct {
	OrderID    uuid.UUID
	CallerID   uuid.UUID
	CallerRole string
	Items      []OrderItemInput
}

func (s *OrderService) Update(ctx context.Context, in UpdateOrderInput) (OrderWithItems, error) {
	order, err := s.q.GetOrderByID(ctx, in.OrderID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrderWithItems{}, apierr.New(apierr.ErrOrderNotFound, "Заказ не найден")
		}
		return OrderWithItems{}, fmt.Errorf("get order: %w", err)
	}
	if in.CallerRole != "ADMIN" && order.UserID != in.CallerID {
		return OrderWithItems{}, apierr.New(apierr.ErrOrderOwnershipViolation, "Заказ принадлежит другому пользователю")
	}

	if order.Status != "CREATED" {
		return OrderWithItems{}, apierr.New(apierr.ErrInvalidStateTransition, "Обновление разрешено только в статусе CREATED")
	}

	if err := s.checkRateLimit(ctx, in.CallerID, "UPDATE_ORDER"); err != nil {
		return OrderWithItems{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.q.WithTx(tx)

	oldItems, err := qtx.GetOrderItems(ctx, in.OrderID)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("get old items: %w", err)
	}
	for _, oi := range oldItems {
		if err := qtx.IncrementStock(ctx, sqlcdb.IncrementStockParams{
			ID:    oi.ProductID,
			Stock: oi.Quantity,
		}); err != nil {
			return OrderWithItems{}, fmt.Errorf("restore stock: %w", err)
		}
	}

	type productEntry struct {
		product  sqlcdb.Product
		quantity int32
	}
	var entries []productEntry
	var stockErrors []map[string]any

	for _, item := range in.Items {
		p, err := qtx.GetProductByIDForUpdate(ctx, item.ProductID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return OrderWithItems{}, apierr.New(apierr.ErrProductNotFound, fmt.Sprintf("Товар %s не найден", item.ProductID))
			}
			return OrderWithItems{}, fmt.Errorf("get product: %w", err)
		}
		if p.Status != "ACTIVE" {
			return OrderWithItems{}, apierr.New(apierr.ErrProductInactive, fmt.Sprintf("Товар %s неактивен", item.ProductID))
		}
		if p.Stock < item.Quantity {
			stockErrors = append(stockErrors, map[string]any{
				"product_id": item.ProductID.String(),
				"requested":  item.Quantity,
				"available":  p.Stock,
			})
		}
		entries = append(entries, productEntry{product: p, quantity: item.Quantity})
	}
	if len(stockErrors) > 0 {
		return OrderWithItems{}, apierr.NewWithDetails(apierr.ErrInsufficientStock,
			"Недостаточно товара на складе",
			map[string]any{"items": stockErrors})
	}

	total := decimal.Zero
	for i := range entries {
		if err := qtx.DecrementStock(ctx, sqlcdb.DecrementStockParams{
			ID:    entries[i].product.ID,
			Stock: entries[i].quantity,
		}); err != nil {
			return OrderWithItems{}, fmt.Errorf("decrement stock: %w", err)
		}
		total = total.Add(entries[i].product.Price.Mul(decimal.NewFromInt(int64(entries[i].quantity))))
	}

	discount := decimal.Zero
	promoCodeID := order.PromoCodeID

	if order.PromoCodeID.Valid {
		promoID := uuid.UUID(order.PromoCodeID.Bytes)
		promo, err := qtx.GetPromoCodeByIDForUpdate(ctx, promoID)
		if err == nil {
			now := time.Now()
			if promo.Active && promo.CurrentUses <= promo.MaxUses &&
				!now.Before(promo.ValidFrom.Time) && !now.After(promo.ValidUntil.Time) &&
				total.GreaterThanOrEqual(promo.MinOrderAmount) {
				discount = calcDiscount(total, promo)
			} else {
				// promo no longer applicable — refund one use
				_ = qtx.DecrementPromoUses(ctx, promoID)
				promoCodeID = pgtype.UUID{}
			}
		}
	}
	total = total.Sub(discount)

	if err := qtx.DeleteOrderItems(ctx, in.OrderID); err != nil {
		return OrderWithItems{}, fmt.Errorf("delete old items: %w", err)
	}

	updatedOrder, err := qtx.UpdateOrderAmounts(ctx, sqlcdb.UpdateOrderAmountsParams{
		ID:             in.OrderID,
		TotalAmount:    total,
		DiscountAmount: discount,
		PromoCodeID:    promoCodeID,
	})
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("update order amounts: %w", err)
	}

	var orderItems []sqlcdb.OrderItem
	for i := range entries {
		oi, err := qtx.CreateOrderItem(ctx, sqlcdb.CreateOrderItemParams{
			OrderID:      in.OrderID,
			ProductID:    entries[i].product.ID,
			Quantity:     entries[i].quantity,
			PriceAtOrder: entries[i].product.Price,
		})
		if err != nil {
			return OrderWithItems{}, fmt.Errorf("create item: %w", err)
		}
		orderItems = append(orderItems, oi)
	}

	if _, err := qtx.CreateUserOperation(ctx, sqlcdb.CreateUserOperationParams{
		UserID:        in.CallerID,
		OperationType: "UPDATE_ORDER",
	}); err != nil {
		return OrderWithItems{}, fmt.Errorf("record operation: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return OrderWithItems{}, fmt.Errorf("commit: %w", err)
	}

	return OrderWithItems{Order: updatedOrder, Items: orderItems}, nil
}

func (s *OrderService) Cancel(ctx context.Context, id uuid.UUID, callerID uuid.UUID, callerRole string) (OrderWithItems, error) {
	order, err := s.q.GetOrderByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OrderWithItems{}, apierr.New(apierr.ErrOrderNotFound, "Заказ не найден")
		}
		return OrderWithItems{}, fmt.Errorf("get order: %w", err)
	}

	if callerRole != "ADMIN" && order.UserID != callerID {
		return OrderWithItems{}, apierr.New(apierr.ErrOrderOwnershipViolation, "Заказ принадлежит другому пользователю")
	}

	if order.Status != "CREATED" && order.Status != "PAYMENT_PENDING" {
		return OrderWithItems{}, apierr.New(apierr.ErrInvalidStateTransition, "Отмена заказа невозможна в текущем состоянии")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	qtx := s.q.WithTx(tx)

	items, err := qtx.GetOrderItems(ctx, id)
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("get items: %w", err)
	}
	for _, oi := range items {
		if err := qtx.IncrementStock(ctx, sqlcdb.IncrementStockParams{
			ID:    oi.ProductID,
			Stock: oi.Quantity,
		}); err != nil {
			return OrderWithItems{}, fmt.Errorf("restore stock: %w", err)
		}
	}

	if order.PromoCodeID.Valid {
		_ = qtx.DecrementPromoUses(ctx, uuid.UUID(order.PromoCodeID.Bytes))
	}

	updatedOrder, err := qtx.UpdateOrderStatus(ctx, sqlcdb.UpdateOrderStatusParams{
		ID:     id,
		Status: "CANCELED",
	})
	if err != nil {
		return OrderWithItems{}, fmt.Errorf("update status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return OrderWithItems{}, fmt.Errorf("commit: %w", err)
	}

	return OrderWithItems{Order: updatedOrder, Items: items}, nil
}

// checkRateLimit returns ORDER_LIMIT_EXCEEDED if the user performed the operation recently.
func (s *OrderService) checkRateLimit(ctx context.Context, userID uuid.UUID, opType string) error {
	op, err := s.q.GetLastUserOperation(ctx, sqlcdb.GetLastUserOperationParams{
		UserID:        userID,
		OperationType: opType,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("check rate limit: %w", err)
	}
	if err == nil {
		limit := time.Duration(s.rateLimitMinutes) * time.Minute
		if time.Since(op.CreatedAt.Time) < limit {
			remaining := int(limit.Seconds() - time.Since(op.CreatedAt.Time).Seconds())
			return apierr.NewWithDetails(apierr.ErrOrderLimitExceeded, "Слишком частые запросы. Попробуйте позже",
				map[string]any{"retry_after_seconds": remaining})
		}
	}
	return nil
}

func calcDiscount(total decimal.Decimal, promo sqlcdb.PromoCode) decimal.Decimal {
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
