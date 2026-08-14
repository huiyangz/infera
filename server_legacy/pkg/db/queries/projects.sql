-- name: CreateProject :one
INSERT INTO projects (name, repo_url, default_branch)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $2, repo_url = $3, default_branch = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = $1;

-- name: GetProjectByDeliveryID :one
SELECT p.* FROM projects p
JOIN deliveries d ON d.project_id = p.id
WHERE d.id = $1;
