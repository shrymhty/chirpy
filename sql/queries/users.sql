-- name: CreateUser :one
INSERT INTO users (
    id,
    created_at,
    updated_at,
    email
) VALUES (
    GEN_RANDOM_UUID(),
    $1,
    $2,
    $3
) RETURNING *;

-- name: DeleteUsers :exec
DELETE FROM users;