-- name: CreatePromoCode :one
INSERT INTO promo_codes (code, discount_type, discount_value, min_order_amount, max_uses, valid_from, valid_until)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetPromoCodeByCode :one
SELECT * FROM promo_codes WHERE code = $1 LIMIT 1;

-- name: GetPromoCodeByIDForUpdate :one
SELECT * FROM promo_codes WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: IncrementPromoUses :exec
UPDATE promo_codes SET current_uses = current_uses + 1 WHERE id = $1;

-- name: DecrementPromoUses :exec
UPDATE promo_codes SET current_uses = current_uses - 1 WHERE id = $1 AND current_uses > 0;
