-- name: CreateOrder :one
INSERT INTO orders (user_id, status, promo_code_id, total_amount, discount_amount)
VALUES ($1, 'CREATED', $2, $3, $4)
RETURNING *;

-- name: GetOrderByID :one
SELECT * FROM orders WHERE id = $1 LIMIT 1;

-- name: GetOrderByIDForUpdate :one
SELECT * FROM orders WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: GetActiveOrderByUserID :one
SELECT * FROM orders
WHERE user_id = $1
  AND status IN ('CREATED', 'PAYMENT_PENDING')
LIMIT 1;

-- name: UpdateOrderStatus :one
UPDATE orders SET status = $2 WHERE id = $1 RETURNING *;

-- name: UpdateOrderAmounts :one
UPDATE orders
SET total_amount    = $2,
    discount_amount = $3,
    promo_code_id   = $4
WHERE id = $1
RETURNING *;

-- name: CreateOrderItem :one
INSERT INTO order_items (order_id, product_id, quantity, price_at_order)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetOrderItems :many
SELECT * FROM order_items WHERE order_id = $1 ORDER BY id;

-- name: DeleteOrderItems :exec
DELETE FROM order_items WHERE order_id = $1;
