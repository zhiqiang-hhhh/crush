-- name: GetMessage :one
SELECT *
FROM messages
WHERE id = ? LIMIT 1;

-- name: ListMessagesBySession :many
SELECT *
FROM messages
WHERE session_id = ?
ORDER BY rowid ASC;

-- name: CreateMessage :one
INSERT INTO messages (
    id,
    session_id,
    role,
    parts,
    model,
    provider,
    is_summary_message,
    agent_name,
    created_at,
    updated_at
) VALUES (
    ?, ?, ?, ?, ?, ?, ?, ?, strftime('%s', 'now'), strftime('%s', 'now')
)
RETURNING *;

-- name: UpdateMessage :exec
UPDATE messages
SET
    parts = ?,
    finished_at = ?,
    updated_at = strftime('%s', 'now')
WHERE id = ?;


-- name: DeleteMessage :exec
DELETE FROM messages
WHERE id = ?;

-- name: DeleteSessionMessages :exec
DELETE FROM messages
WHERE session_id = ?;

-- name: ListUserMessagesBySession :many
SELECT *
FROM messages
WHERE session_id = ? AND role = 'user'
ORDER BY created_at DESC;

-- name: ListAllUserMessages :many
SELECT *
FROM messages
WHERE role = 'user'
ORDER BY created_at DESC;

-- name: ListRecentMessagesBySession :many
SELECT *
FROM messages
WHERE session_id = ?
ORDER BY rowid DESC
LIMIT ?;

-- name: ListMessagesBySessionBefore :many
SELECT m.*
FROM messages m
WHERE m.session_id = ? AND m.rowid < (SELECT mm.rowid FROM messages mm WHERE mm.id = ?)
ORDER BY m.rowid DESC
LIMIT ?;

-- name: GetSummaryMessageID :one
SELECT id
FROM messages
WHERE session_id = ? AND is_summary_message = 1
ORDER BY rowid DESC
LIMIT 1;

-- name: ForkSessionMessages :exec
INSERT INTO messages (id, session_id, role, parts, model, provider, is_summary_message, agent_name, created_at, updated_at, finished_at)
SELECT hex(randomblob(16)), @new_session_id, role, parts, model, provider, is_summary_message, agent_name, created_at, updated_at, finished_at
FROM messages
WHERE messages.session_id = @source_session_id
ORDER BY rowid ASC;
