-- name: CreateProduct :one
INSERT INTO products (name, description, price, stock, category, status, seller_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProductByID :one
SELECT * FROM products WHERE id = $1 LIMIT 1;

-- name: GetProductByIDForUpdate :one
SELECT * FROM products WHERE id = $1 LIMIT 1 FOR UPDATE;

-- name: UpdateProduct :one
UPDATE products
SET name        = $2,
    description = $3,
    price       = $4,
    stock       = $5,
    category    = $6,
    status      = $7
WHERE id = $1
RETURNING *;

-- name: ArchiveProduct :one
UPDATE products SET status = 'ARCHIVED' WHERE id = $1 RETURNING *;

-- name: DecrementStock :exec
UPDATE products SET stock = stock - $2 WHERE id = $1;

-- name: IncrementStock :exec
UPDATE products SET stock = stock + $2 WHERE id = $1;

-- name: ListProducts :many
SELECT * FROM products
WHERE (sqlc.narg(status)::text IS NULL OR status::text = sqlc.narg(status))
  AND (sqlc.narg(category)::text IS NULL OR category = sqlc.narg(category))
ORDER BY created_at DESC
LIMIT sqlc.arg(limit_val) OFFSET sqlc.arg(offset_val);

-- name: CountProducts :one
SELECT COUNT(*) FROM products
WHERE (sqlc.narg(status)::text IS NULL OR status::text = sqlc.narg(status))
  AND (sqlc.narg(category)::text IS NULL OR category = sqlc.narg(category));
