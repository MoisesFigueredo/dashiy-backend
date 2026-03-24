-- name: ListNichesByCompanyID :many
SELECT *
FROM niches
WHERE company_id = $1
ORDER BY name;
