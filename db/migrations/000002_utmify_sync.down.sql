DROP INDEX IF EXISTS idx_commission_entries_collaborator_month;
DROP INDEX IF EXISTS uq_commission_entries_ad_snapshot;
ALTER TABLE commission_entries DROP CONSTRAINT IF EXISTS commission_entries_source_type_check;
DELETE FROM commission_entries WHERE collaborator_id IS NOT NULL OR source_type = 'ad_snapshot';
ALTER TABLE commission_entries
    DROP COLUMN IF EXISTS collaborator_id,
    DROP COLUMN IF EXISTS ad_id,
    DROP COLUMN IF EXISTS snapshot_date,
    DROP COLUMN IF EXISTS revenue_amount,
    DROP COLUMN IF EXISTS spend_amount,
    DROP COLUMN IF EXISTS chargeback_amount,
    DROP COLUMN IF EXISTS source_type,
    DROP COLUMN IF EXISTS metadata;
ALTER TABLE commission_entries
    ALTER COLUMN transaction_id SET NOT NULL;
ALTER TABLE commission_entries
    ALTER COLUMN user_id SET NOT NULL;

ALTER TABLE ad_metric_snapshots
    DROP COLUMN IF EXISTS revenue,
    DROP COLUMN IF EXISTS gross_revenue,
    DROP COLUMN IF EXISTS profit,
    DROP COLUMN IF EXISTS chargeback_amount,
    DROP COLUMN IF EXISTS roas,
    DROP COLUMN IF EXISTS roi,
    DROP COLUMN IF EXISTS cpa,
    DROP COLUMN IF EXISTS approved_orders_count,
    DROP COLUMN IF EXISTS total_orders_count,
    DROP COLUMN IF EXISTS pending_orders_count,
    DROP COLUMN IF EXISTS video_views,
    DROP COLUMN IF EXISTS video_views_3_seconds,
    DROP COLUMN IF EXISTS video_75_watched,
    DROP COLUMN IF EXISTS hook_play_rate,
    DROP COLUMN IF EXISTS icr,
    DROP COLUMN IF EXISTS connect_rate,
    DROP COLUMN IF EXISTS conversion,
    DROP COLUMN IF EXISTS object_status,
    DROP COLUMN IF EXISTS effective_status,
    DROP COLUMN IF EXISTS raw_payload;

DROP INDEX IF EXISTS idx_ad_collaborators_collaborator_id;
DROP INDEX IF EXISTS uq_ad_collaborators_collaborator;
DELETE FROM ad_collaborators WHERE collaborator_id IS NOT NULL;
ALTER TABLE ad_collaborators
    DROP COLUMN IF EXISTS collaborator_id;
ALTER TABLE ad_collaborators
    ALTER COLUMN user_id SET NOT NULL;

DROP INDEX IF EXISTS idx_utmify_dashboards_company_niche;
DROP TABLE IF EXISTS utmify_dashboards;

DROP INDEX IF EXISTS idx_collaborators_company_role;
DROP TABLE IF EXISTS collaborators;
