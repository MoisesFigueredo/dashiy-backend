CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE companies (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    slug text NOT NULL UNIQUE,
    legal_name text,
    tax_id text,
    tax_rate numeric(6,3) NOT NULL DEFAULT 0,
    plan text NOT NULL DEFAULT 'starter',
    active boolean NOT NULL DEFAULT true,
    suspended_at timestamptz,
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE niches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    name text NOT NULL,
    slug text NOT NULL,
    tax_rate numeric(6,3),
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, slug)
);

CREATE TABLE system_users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    totp_secret text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    last_login_at timestamptz,
    last_login_ip text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    code text,
    full_name text NOT NULL,
    email text NOT NULL,
    password_hash text,
    role text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    last_login_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, email),
    UNIQUE (company_id, code),
    CHECK (role IN ('admin', 'gestor_trafego', 'traffic_manager', 'copywriter', 'editor', 'closer', 'gestor_projetos', 'analyst'))
);

CREATE TABLE niche_users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    active boolean NOT NULL DEFAULT true,
    assigned_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (niche_id, user_id)
);

CREATE TABLE platform_integrations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    platform text NOT NULL,
    name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    webhook_token text NOT NULL UNIQUE,
    webhook_secret text,
    access_token text,
    refresh_token text,
    external_account_id text,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_synced_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (platform IN ('hotmart', 'kiwify', 'kirvano', 'cakto', 'payt', 'facebook_ads'))
);

CREATE TABLE ad_accounts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    platform_integration_id uuid REFERENCES platform_integrations(id) ON DELETE SET NULL,
    external_id text NOT NULL,
    name text NOT NULL,
    currency text NOT NULL DEFAULT 'BRL',
    timezone text,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, external_id)
);

CREATE TABLE campaigns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    ad_account_id uuid NOT NULL REFERENCES ad_accounts(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    name text NOT NULL,
    objective text,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, external_id)
);

CREATE TABLE adsets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    campaign_id uuid NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, external_id)
);

CREATE TABLE ads (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    adset_id uuid NOT NULL REFERENCES adsets(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    name text NOT NULL,
    name_parsed jsonb NOT NULL DEFAULT '{}'::jsonb,
    status text NOT NULL DEFAULT 'testing',
    rejection_reason text,
    validated_at timestamptz,
    utm_source text,
    utm_campaign text,
    utm_content text,
    utm_term text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, external_id),
    CHECK (status IN ('testing', 'pre_scale', 'validated', 'rejected'))
);

CREATE TABLE offers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    code text NOT NULL,
    name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, niche_id, code)
);

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    offer_id uuid NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
    platform text NOT NULL,
    external_id text NOT NULL,
    name text NOT NULL,
    active boolean NOT NULL DEFAULT true,
    metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, platform, external_id)
);

CREATE TABLE commission_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    user_id uuid REFERENCES users(id) ON DELETE CASCADE,
    offer_id uuid REFERENCES offers(id) ON DELETE CASCADE,
    role text NOT NULL,
    percentage_min numeric(8,4) NOT NULL,
    percentage_max numeric(8,4) NOT NULL,
    rule_type text NOT NULL DEFAULT 'profit',
    priority integer NOT NULL DEFAULT 100,
    active boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (percentage_min >= 0),
    CHECK (percentage_max >= percentage_min),
    CHECK (rule_type IN ('profit', 'revenue', 'manual'))
);

CREATE TABLE performance_goals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    offer_id uuid REFERENCES offers(id) ON DELETE SET NULL,
    period_month date NOT NULL,
    profit_goal numeric(16,2) NOT NULL,
    created_by uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    platform_integration_id uuid REFERENCES platform_integrations(id) ON DELETE SET NULL,
    platform text NOT NULL,
    company_token text NOT NULL,
    external_event_id text,
    payload_hash text NOT NULL,
    signature text,
    headers jsonb NOT NULL DEFAULT '{}'::jsonb,
    payload jsonb NOT NULL,
    status text NOT NULL DEFAULT 'received',
    error_message text,
    retries integer NOT NULL DEFAULT 0,
    received_at timestamptz NOT NULL DEFAULT now(),
    queued_at timestamptz,
    processed_at timestamptz,
    CHECK (status IN ('received', 'queued', 'processed', 'failed', 'ignored'))
);

CREATE TABLE transactions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    source_delivery_id uuid REFERENCES webhook_deliveries(id) ON DELETE SET NULL,
    platform text NOT NULL,
    platform_tx_id text NOT NULL,
    event_type text NOT NULL,
    amount numeric(16,2) NOT NULL,
    upsell_amount numeric(16,2) NOT NULL DEFAULT 0,
    currency text NOT NULL DEFAULT 'BRL',
    status text NOT NULL,
    attribution_status text NOT NULL DEFAULT 'unattributed',
    buyer_email text,
    buyer_name text,
    ad_id uuid REFERENCES ads(id) ON DELETE SET NULL,
    offer_id uuid REFERENCES offers(id) ON DELETE SET NULL,
    product_id uuid REFERENCES products(id) ON DELETE SET NULL,
    raw_payload jsonb NOT NULL DEFAULT '{}'::jsonb,
    utm_params jsonb NOT NULL DEFAULT '{}'::jsonb,
    occurred_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (company_id, platform, platform_tx_id),
    CHECK (attribution_status IN ('attributed', 'unattributed', 'organic'))
);

CREATE TABLE commission_periods (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    period_month date NOT NULL,
    status text NOT NULL DEFAULT 'open',
    closed_by uuid REFERENCES users(id) ON DELETE SET NULL,
    closed_at timestamptz,
    notes text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (status IN ('open', 'closed'))
);

CREATE TABLE commission_entries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid REFERENCES niches(id) ON DELETE SET NULL,
    transaction_id uuid NOT NULL REFERENCES transactions(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    commission_period_id uuid REFERENCES commission_periods(id) ON DELETE SET NULL,
    role text NOT NULL,
    base_amount numeric(16,2) NOT NULL,
    commission_pct numeric(8,4) NOT NULL,
    commission_value numeric(16,2) NOT NULL,
    status text NOT NULL DEFAULT 'pending',
    adjustment_reason text,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (transaction_id, user_id, role),
    CHECK (status IN ('pending', 'approved', 'reversed', 'adjusted'))
);

CREATE TABLE ad_metric_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    ad_id uuid NOT NULL REFERENCES ads(id) ON DELETE CASCADE,
    snapshot_date date NOT NULL,
    impressions bigint NOT NULL DEFAULT 0,
    clicks bigint NOT NULL DEFAULT 0,
    spend numeric(16,2) NOT NULL DEFAULT 0,
    cpc numeric(16,4) NOT NULL DEFAULT 0,
    cpm numeric(16,4) NOT NULL DEFAULT 0,
    ctr numeric(10,4) NOT NULL DEFAULT 0,
    reach bigint NOT NULL DEFAULT 0,
    frequency numeric(10,4) NOT NULL DEFAULT 0,
    hook_rate numeric(10,4) NOT NULL DEFAULT 0,
    body_rate numeric(10,4) NOT NULL DEFAULT 0,
    view_page bigint NOT NULL DEFAULT 0,
    initiate_checkout bigint NOT NULL DEFAULT 0,
    cost_per_ic numeric(16,4) NOT NULL DEFAULT 0,
    fetched_at timestamptz NOT NULL DEFAULT now(),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (ad_id, snapshot_date)
);

CREATE TABLE audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id uuid REFERENCES users(id) ON DELETE SET NULL,
    entity_type text NOT NULL,
    entity_id uuid,
    action text NOT NULL,
    old_value jsonb,
    new_value jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE ad_collaborators (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    niche_id uuid NOT NULL REFERENCES niches(id) ON DELETE CASCADE,
    ad_id uuid NOT NULL REFERENCES ads(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role text NOT NULL,
    commission_pct_min numeric(8,4) NOT NULL DEFAULT 0,
    commission_pct_max numeric(8,4) NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (ad_id, user_id, role)
);

CREATE TABLE impersonation_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_user_id uuid NOT NULL REFERENCES system_users(id) ON DELETE CASCADE,
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    impersonated_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason text NOT NULL,
    ip_address text NOT NULL,
    started_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz
);

CREATE TABLE system_audit_log (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    system_user_id uuid REFERENCES system_users(id) ON DELETE SET NULL,
    impersonation_id uuid REFERENCES impersonation_sessions(id) ON DELETE SET NULL,
    action text NOT NULL,
    target_type text,
    target_id uuid,
    payload jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE company_feature_flags (
    company_id uuid NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    feature text NOT NULL,
    enabled boolean NOT NULL DEFAULT false,
    enabled_by uuid REFERENCES system_users(id) ON DELETE SET NULL,
    enabled_at timestamptz,
    PRIMARY KEY (company_id, feature)
);

CREATE UNIQUE INDEX uq_performance_goals_scope
    ON performance_goals (
        company_id,
        COALESCE(niche_id, '00000000-0000-0000-0000-000000000000'::uuid),
        COALESCE(offer_id, '00000000-0000-0000-0000-000000000000'::uuid),
        period_month
    );

CREATE UNIQUE INDEX uq_commission_periods_scope
    ON commission_periods (
        company_id,
        COALESCE(niche_id, '00000000-0000-0000-0000-000000000000'::uuid),
        period_month
    );

CREATE INDEX idx_niches_company_id ON niches (company_id);
CREATE INDEX idx_users_company_id ON users (company_id);
CREATE INDEX idx_users_company_code ON users (company_id, code);
CREATE INDEX idx_niche_users_lookup ON niche_users (company_id, niche_id, user_id);
CREATE INDEX idx_platform_integrations_company_platform ON platform_integrations (company_id, platform);
CREATE INDEX idx_ad_accounts_company_niche ON ad_accounts (company_id, niche_id);
CREATE INDEX idx_campaigns_company_niche ON campaigns (company_id, niche_id);
CREATE INDEX idx_adsets_company_niche ON adsets (company_id, niche_id);
CREATE INDEX idx_ads_company_niche ON ads (company_id, niche_id);
CREATE INDEX idx_ads_lookup_name ON ads (company_id, name);
CREATE INDEX idx_offers_company_niche ON offers (company_id, niche_id);
CREATE INDEX idx_products_offer_id ON products (offer_id);
CREATE INDEX idx_commission_rules_lookup ON commission_rules (company_id, role, active);
CREATE INDEX idx_performance_goals_company_period ON performance_goals (company_id, period_month);
CREATE INDEX idx_webhook_deliveries_company_status ON webhook_deliveries (company_id, status, received_at DESC);
CREATE INDEX idx_webhook_deliveries_platform_token ON webhook_deliveries (platform, company_token);
CREATE INDEX idx_transactions_company_niche_occurred ON transactions (company_id, niche_id, occurred_at DESC);
CREATE INDEX idx_transactions_ad_id ON transactions (ad_id);
CREATE INDEX idx_transactions_offer_id ON transactions (offer_id);
CREATE INDEX idx_transactions_status ON transactions (company_id, status);
CREATE INDEX idx_commission_entries_company_status ON commission_entries (company_id, status);
CREATE INDEX idx_commission_entries_user_period ON commission_entries (user_id, commission_period_id);
CREATE INDEX idx_ad_metric_snapshots_scope ON ad_metric_snapshots (company_id, niche_id, snapshot_date DESC);
CREATE INDEX idx_audit_log_company_created_at ON audit_log (company_id, created_at DESC);
CREATE INDEX idx_ad_collaborators_ad_id ON ad_collaborators (ad_id);
CREATE INDEX idx_impersonation_sessions_company_started_at ON impersonation_sessions (company_id, started_at DESC);
CREATE INDEX idx_system_audit_log_created_at ON system_audit_log (created_at DESC);
