-- name: ListOffersByCompanyID :many
SELECT *
FROM offers
WHERE company_id = $1
ORDER BY name;

-- name: ListOffersByNicheID :many
SELECT *
FROM offers
WHERE company_id = $1
  AND niche_id = $2
ORDER BY name;

-- name: GetOfferByCode :one
SELECT *
FROM offers
WHERE company_id = $1
  AND niche_id = $2
  AND code = $3
LIMIT 1;
