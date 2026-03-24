-- name: ListTransactionsByCompanyID :many
SELECT *
FROM transactions
WHERE company_id = $1
ORDER BY occurred_at DESC;

-- name: ListTransactionsByNicheID :many
SELECT *
FROM transactions
WHERE company_id = $1
  AND niche_id = $2
ORDER BY occurred_at DESC;

-- name: UpsertTransaction :one
INSERT INTO transactions (
    company_id,
    niche_id,
    source_delivery_id,
    platform,
    platform_tx_id,
    event_type,
    amount,
    upsell_amount,
    currency,
    status,
    attribution_status,
    buyer_email,
    buyer_name,
    ad_id,
    offer_id,
    product_id,
    raw_payload,
    utm_params,
    occurred_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19
)
ON CONFLICT (company_id, platform, platform_tx_id)
DO UPDATE SET
    niche_id = EXCLUDED.niche_id,
    source_delivery_id = EXCLUDED.source_delivery_id,
    event_type = EXCLUDED.event_type,
    amount = EXCLUDED.amount,
    upsell_amount = EXCLUDED.upsell_amount,
    currency = EXCLUDED.currency,
    status = EXCLUDED.status,
    attribution_status = EXCLUDED.attribution_status,
    buyer_email = EXCLUDED.buyer_email,
    buyer_name = EXCLUDED.buyer_name,
    ad_id = EXCLUDED.ad_id,
    offer_id = EXCLUDED.offer_id,
    product_id = EXCLUDED.product_id,
    raw_payload = EXCLUDED.raw_payload,
    utm_params = EXCLUDED.utm_params,
    occurred_at = EXCLUDED.occurred_at,
    updated_at = now()
RETURNING *;
