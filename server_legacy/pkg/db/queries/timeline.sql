-- name: CreateTimelineEvent :one
INSERT INTO timeline_events (delivery_id, stage, event_type, payload)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListTimelineEvents :many
SELECT * FROM timeline_events
WHERE delivery_id = $1
ORDER BY created_at ASC;
