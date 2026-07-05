-- name: CreateUser :one
INSERT INTO users (
    id,
    created_at,
    updated_at,
    email,
    hashed_password
) VALUES (
    GEN_RANDOM_UUID(),
    $1,
    $2,
    $3,
    $4
) RETURNING *;

-- name: DeleteUsers :exec
DELETE FROM users;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;