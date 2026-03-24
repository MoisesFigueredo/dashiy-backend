-- name: GetProductByPlatformExternalID :one
SELECT *
FROM products
WHERE company_id = $1
  AND platform = $2
  AND external_id = $3
LIMIT 1;
