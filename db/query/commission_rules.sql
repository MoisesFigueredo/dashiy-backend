-- name: ListCommissionRuleCandidates :many
SELECT *
FROM commission_rules
WHERE company_id = $1
  AND role = $2
  AND active = TRUE
  AND (niche_id IS NULL OR niche_id = $3)
  AND (user_id IS NULL OR user_id = $4)
  AND (
    offer_id IS NULL
    OR (sqlc.narg(offer_id)::uuid IS NOT NULL AND offer_id = sqlc.narg(offer_id)::uuid)
  )
ORDER BY
  CASE
    WHEN user_id IS NOT NULL AND offer_id IS NOT NULL THEN 1
    WHEN user_id IS NOT NULL THEN 2
    WHEN offer_id IS NOT NULL THEN 3
    ELSE 4
  END,
  priority ASC,
  created_at ASC;

-- name: UpsertCommissionEntry :one
INSERT INTO commission_entries (
    company_id,
    niche_id,
    transaction_id,
    user_id,
    role,
    base_amount,
    commission_pct,
    commission_value,
    status,
    adjustment_reason
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (transaction_id, user_id, role)
DO UPDATE SET
    niche_id = EXCLUDED.niche_id,
    base_amount = EXCLUDED.base_amount,
    commission_pct = EXCLUDED.commission_pct,
    commission_value = EXCLUDED.commission_value,
    status = EXCLUDED.status,
    adjustment_reason = EXCLUDED.adjustment_reason,
    updated_at = now()
RETURNING *;
