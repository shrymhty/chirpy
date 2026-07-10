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

-- name: UpdateUser :one
UPDATE users
SET
email = $1,
hashed_password=$2,
updated_at = $3
WHERE id = $4
RETURNING *;

-- name: UpgradeUserToChirpyRed :one
UPDATE users    
SET
is_chirpy_red = TRUE,
updated_at = $1
WHERE id = $2
RETURNING *;