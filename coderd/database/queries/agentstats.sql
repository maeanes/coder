-- name: InsertAgentStat :one
INSERT INTO
	agent_stats (
		id,
		created_at,
		user_id,
		workspace_id,
		agent_id,
		payload
	)
VALUES
	($1, $2, $3, $4, $5, $6) RETURNING *;
