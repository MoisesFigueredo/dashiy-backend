-- Dashboard summary snapshots: stores the raw MCP dashboard summary per dashboard per date.
-- Replaces volatile Redis cache as the primary data source for GET /dashboard/summary.
CREATE TABLE dashboard_summary_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_external_id text NOT NULL,
    dashboard_name text NOT NULL,
    currency text NOT NULL DEFAULT 'BRL',
    time_zone integer NOT NULL DEFAULT 0,
    snapshot_date date NOT NULL,
    raw_summary jsonb NOT NULL,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dashboard_external_id, snapshot_date)
);

CREATE INDEX idx_dashboard_summary_snapshots_date
    ON dashboard_summary_snapshots (snapshot_date);

-- Ad object snapshots: stores the raw MCP ad objects per dashboard+level+date.
-- Replaces volatile Redis cache as the primary data source for GET /ads/objects.
CREATE TABLE ad_object_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    dashboard_external_id text NOT NULL,
    dashboard_name text NOT NULL,
    currency text NOT NULL DEFAULT 'BRL',
    level text NOT NULL,
    snapshot_date date NOT NULL,
    raw_objects jsonb NOT NULL DEFAULT '[]'::jsonb,
    object_count integer NOT NULL DEFAULT 0,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (dashboard_external_id, level, snapshot_date),
    CHECK (level IN ('account', 'campaign', 'adset', 'ad'))
);

CREATE INDEX idx_ad_object_snapshots_date_level
    ON ad_object_snapshots (snapshot_date, level);
