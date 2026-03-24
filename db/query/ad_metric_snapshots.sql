-- name: ListAdMetricSnapshotsByCompanyID :many
SELECT *
FROM ad_metric_snapshots
WHERE company_id = $1
ORDER BY snapshot_date DESC, ad_id;

-- name: ListAdMetricSnapshotsByNicheID :many
SELECT *
FROM ad_metric_snapshots
WHERE company_id = $1
  AND niche_id = $2
ORDER BY snapshot_date DESC, ad_id;
