-- name: ListUsersByCompanyID :many
SELECT *
FROM users
WHERE company_id = $1
  AND active = TRUE
ORDER BY role, full_name;

-- name: ListUsersByNicheID :many
SELECT u.*
FROM users u
INNER JOIN niche_users nu
    ON nu.user_id = u.id
   AND nu.company_id = u.company_id
WHERE u.company_id = $1
  AND nu.niche_id = $2
  AND u.active = TRUE
  AND nu.active = TRUE
ORDER BY u.role, u.full_name;

-- name: ListUsersByCodes :many
SELECT *
FROM users
WHERE company_id = $1
  AND code = ANY($2::text[])
  AND active = TRUE
ORDER BY full_name;
