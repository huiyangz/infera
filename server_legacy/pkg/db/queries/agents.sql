-- name: GetAgentByRole :one
SELECT * FROM agent_configs WHERE role = $1 LIMIT 1;
