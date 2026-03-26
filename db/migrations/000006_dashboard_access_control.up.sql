-- Dashboard access control: allows admin to override collaborator access to dashboards.
-- By default, collaborators see dashboards based on ad_collaborators linkage.
-- An entry here with allowed=false blocks the collaborator even if linked via ads.
-- An entry with allowed=true is a no-op (matches default behavior) but is stored
-- so the admin UI can show explicit "allowed" state.

CREATE TABLE IF NOT EXISTS dashboard_access_overrides (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id  UUID        NOT NULL REFERENCES companies(id),
    user_id     UUID        NOT NULL REFERENCES users(id),
    dashboard_id TEXT       NOT NULL,  -- utmify_dashboards.external_id
    allowed     BOOLEAN     NOT NULL DEFAULT true,
    updated_by  UUID        REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_dashboard_access_override UNIQUE (company_id, user_id, dashboard_id)
);

CREATE INDEX idx_dashboard_access_overrides_lookup
    ON dashboard_access_overrides (company_id, user_id, allowed);
