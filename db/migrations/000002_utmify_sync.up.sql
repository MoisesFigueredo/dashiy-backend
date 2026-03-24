CREATE TABLE collaborators (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name text NOT NULL,
    code text NOT NULL,
    role text NOT NULL,
    commission_rate_min numeric(8,4) NOT NULL DEFAULT 0,
    commission_rate_max numeric(8,4) NOT NULL DEFAULT 0,
    pix_key text,
    salary_base numeric(16,2) NOT NULL DEFAULT 0,
    whatsapp text,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, code),
    CHECK (commission_rate_min >= 0),
    CHECK (commission_rate_max >= commission_rate_min),
    CHECK (role IN ('copywriter', 'editor', 'gestor_trafego', 'desenvolvedor', 'closer')),
    CHECK (status IN ('active', 'inactive'))
);

CREATE INDEX idx_collaborators_company_role ON collaborators (company_id, role, status);

CREATE TABLE utmify_dashboards (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    name text NOT NULL,
    time_zone integer NOT NULL DEFAULT 0,
    currency text NOT NULL DEFAULT 'BRL',
    view_type text NOT NULL DEFAULT 'Normal',
    active boolean NOT NULL DEFAULT true,
    platforms jsonb NOT NULL DEFAULT '[]'::jsonb,
    products jsonb NOT NULL DEFAULT '[]'::jsonb,
    last_synced_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, external_id),
    CHECK (view_type IN ('Total', 'Normal'))
);

CREATE INDEX idx_utmify_dashboards_company_niche ON utmify_dashboards (company_id, niche_id);

ALTER TABLE ad_collaborators
    ADD COLUMN collaborator_id uuid REFERENCES collaborators(id) ON DELETE CASCADE;

ALTER TABLE ad_collaborators
    ALTER COLUMN user_id DROP NOT NULL;

CREATE UNIQUE INDEX uq_ad_collaborators_collaborator
    ON ad_collaborators (ad_id, collaborator_id, role)
    WHERE collaborator_id IS NOT NULL;

CREATE INDEX idx_ad_collaborators_collaborator_id
    ON ad_collaborators (collaborator_id)
    WHERE collaborator_id IS NOT NULL;

ALTER TABLE ad_metric_snapshots
    ADD COLUMN revenue numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN gross_revenue numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN profit numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN chargeback_amount numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN roas numeric(16,4) NOT NULL DEFAULT 0,
    ADD COLUMN roi numeric(16,4) NOT NULL DEFAULT 0,
    ADD COLUMN cpa numeric(16,4) NOT NULL DEFAULT 0,
    ADD COLUMN approved_orders_count bigint NOT NULL DEFAULT 0,
    ADD COLUMN total_orders_count bigint NOT NULL DEFAULT 0,
    ADD COLUMN pending_orders_count bigint NOT NULL DEFAULT 0,
    ADD COLUMN video_views bigint NOT NULL DEFAULT 0,
    ADD COLUMN video_views_3_seconds bigint NOT NULL DEFAULT 0,
    ADD COLUMN video_75_watched bigint NOT NULL DEFAULT 0,
    ADD COLUMN hook_play_rate numeric(10,6) NOT NULL DEFAULT 0,
    ADD COLUMN icr numeric(10,6) NOT NULL DEFAULT 0,
    ADD COLUMN connect_rate numeric(10,6) NOT NULL DEFAULT 0,
    ADD COLUMN conversion numeric(10,6) NOT NULL DEFAULT 0,
    ADD COLUMN object_status text NOT NULL DEFAULT '',
    ADD COLUMN effective_status text NOT NULL DEFAULT '',
    ADD COLUMN raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE commission_entries
    ALTER COLUMN transaction_id DROP NOT NULL;

ALTER TABLE commission_entries
    ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE commission_entries
    ADD COLUMN collaborator_id uuid REFERENCES collaborators(id) ON DELETE CASCADE,
    ADD COLUMN ad_id uuid REFERENCES ads(id) ON DELETE CASCADE,
    ADD COLUMN snapshot_date date,
    ADD COLUMN revenue_amount numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN spend_amount numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN chargeback_amount numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN source_type text NOT NULL DEFAULT 'transaction',
    ADD COLUMN metadata jsonb NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE commission_entries
    ADD CONSTRAINT commission_entries_source_type_check
    CHECK (source_type IN ('transaction', 'ad_snapshot'));

CREATE UNIQUE INDEX uq_commission_entries_ad_snapshot
    ON commission_entries (ad_id, snapshot_date, collaborator_id, role, source_type)
    WHERE source_type = 'ad_snapshot'
      AND ad_id IS NOT NULL
      AND snapshot_date IS NOT NULL
      AND collaborator_id IS NOT NULL;

CREATE INDEX idx_commission_entries_collaborator_month
    ON commission_entries (collaborator_id, snapshot_date DESC)
    WHERE collaborator_id IS NOT NULL;
