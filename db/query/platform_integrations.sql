-- name: GetPlatformIntegrationByWebhookToken :one
SELECT *
FROM platform_integrations
WHERE platform = $1
  AND webhook_token = $2
  AND active = TRUE
LIMIT 1;
