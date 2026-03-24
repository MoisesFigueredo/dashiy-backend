-- name: ListCommissionEntriesByCompanyID :many
SELECT *
FROM commission_entries
WHERE company_id = $1
ORDER BY created_at DESC, user_id;

-- name: ListCommissionEntriesByNicheID :many
SELECT *
FROM commission_entries
WHERE company_id = $1
  AND niche_id = $2
ORDER BY created_at DESC, user_id;
