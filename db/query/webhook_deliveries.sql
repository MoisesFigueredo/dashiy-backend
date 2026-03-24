-- name: CreateWebhookDelivery :one
INSERT INTO webhook_deliveries (
    company_id,
    platform_integration_id,
    platform,
    company_token,
    external_event_id,
    payload_hash,
    signature,
    headers,
    payload
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: MarkWebhookDeliveryQueued :exec
UPDATE webhook_deliveries
SET status = 'queued',
    queued_at = now(),
    error_message = NULL
WHERE id = $1;

-- name: MarkWebhookDeliveryProcessed :exec
UPDATE webhook_deliveries
SET status = 'processed',
    processed_at = now(),
    error_message = NULL
WHERE id = $1;

-- name: MarkWebhookDeliveryFailed :exec
UPDATE webhook_deliveries
SET status = 'failed',
    error_message = $2,
    retries = retries + 1
WHERE id = $1;

-- name: GetWebhookDeliveryContext :one
SELECT sqlc.embed(webhook_deliveries), sqlc.embed(platform_integrations)
FROM webhook_deliveries
LEFT JOIN platform_integrations ON platform_integrations.id = webhook_deliveries.platform_integration_id
WHERE webhook_deliveries.id = $1
LIMIT 1;
