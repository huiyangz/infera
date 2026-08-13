-- name: CreateDelivery :one
INSERT INTO deliveries (project_id, title, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetDelivery :one
SELECT * FROM deliveries WHERE id = $1;

-- name: ListDeliveries :many
SELECT * FROM deliveries ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: UpdateDeliveryStage :one
UPDATE deliveries
SET current_stage = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateDeliveryStatus :one
UPDATE deliveries
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: IncrementDeliveryFailCount :one
UPDATE deliveries
SET fail_count = fail_count + 1, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ResetDeliveryFailCount :one
UPDATE deliveries
SET fail_count = 0, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetDeliveryPendingGate :one
UPDATE deliveries SET pending_gate = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ClearDeliveryPendingGate :one
UPDATE deliveries SET pending_gate = NULL, updated_at = now() WHERE id = $1 RETURNING *;

-- name: ListDeliveriesByProject :many
SELECT * FROM deliveries WHERE project_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3;
