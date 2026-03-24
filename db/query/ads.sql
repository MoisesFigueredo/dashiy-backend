-- name: ListAdsByCompanyID :many
SELECT *
FROM ads
WHERE company_id = $1
ORDER BY updated_at DESC, name;

-- name: ListAdsByNicheID :many
SELECT *
FROM ads
WHERE company_id = $1
  AND niche_id = $2
ORDER BY updated_at DESC, name;

-- name: GetAdByExternalIDOrName :one
SELECT *
FROM ads
WHERE company_id = $1
  AND (
    external_id = $2
    OR lower(name) = lower($3)
    OR lower(COALESCE(utm_content, '')) = lower($3)
  )
ORDER BY
  CASE
    WHEN external_id = $2 THEN 0
    WHEN lower(name) = lower($3) THEN 1
    ELSE 2
  END
LIMIT 1;

-- name: ListAdCollaboratorsByAdID :many
SELECT *
FROM ad_collaborators
WHERE ad_id = $1
ORDER BY role, created_at;

-- name: UpsertAdCollaborator :one
INSERT INTO ad_collaborators (
    company_id,
    niche_id,
    ad_id,
    user_id,
    role,
    commission_pct_min,
    commission_pct_max
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (ad_id, user_id, role)
DO UPDATE SET
    commission_pct_min = EXCLUDED.commission_pct_min,
    commission_pct_max = EXCLUDED.commission_pct_max,
    updated_at = now()
RETURNING *;
