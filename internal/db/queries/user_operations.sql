-- name: GetLastUserOperation :one
SELECT * FROM user_operations
WHERE user_id = $1
  AND operation_type = $2
ORDER BY created_at DESC
LIMIT 1;

-- name: CreateUserOperation :one
INSERT INTO user_operations (user_id, operation_type)
VALUES ($1, $2)
RETURNING *;
