-- Reverse: this is a best-effort rollback

-- 1. Restore company-scoped email uniqueness
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_email_unique;
ALTER TABLE users ADD CONSTRAINT users_company_id_email_key UNIQUE (company_id, email);

-- 2. Recreate collaborators table
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

-- 3. Restore collaborator_id on users
ALTER TABLE users ADD COLUMN collaborator_id uuid REFERENCES collaborators(id);
CREATE UNIQUE INDEX idx_users_collaborator_id ON users(collaborator_id) WHERE collaborator_id IS NOT NULL;

-- 4. Restore collaborator_id on ad_collaborators
ALTER TABLE ad_collaborators ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE ad_collaborators ADD COLUMN collaborator_id uuid REFERENCES collaborators(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX uq_ad_collaborators_collaborator ON ad_collaborators (ad_id, collaborator_id, role) WHERE collaborator_id IS NOT NULL;
CREATE INDEX idx_ad_collaborators_collaborator_id ON ad_collaborators (collaborator_id) WHERE collaborator_id IS NOT NULL;

-- 5. Restore collaborator_id on commission_entries
ALTER TABLE commission_entries ADD COLUMN collaborator_id uuid REFERENCES collaborators(id) ON DELETE CASCADE;
DROP INDEX IF EXISTS uq_commission_entries_ad_snapshot;
DROP INDEX IF EXISTS idx_commission_entries_user_month;
CREATE UNIQUE INDEX uq_commission_entries_ad_snapshot
    ON commission_entries (ad_id, snapshot_date, collaborator_id, role, source_type)
    WHERE source_type = 'ad_snapshot'
      AND ad_id IS NOT NULL
      AND snapshot_date IS NOT NULL
      AND collaborator_id IS NOT NULL;
CREATE INDEX idx_commission_entries_collaborator_month
    ON commission_entries (collaborator_id, snapshot_date DESC)
    WHERE collaborator_id IS NOT NULL;

-- 6. Drop new columns from users
DROP INDEX IF EXISTS idx_users_company_role_status;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_commission_rate_max_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_commission_rate_min_check;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_status_check;
ALTER TABLE users DROP COLUMN IF EXISTS commission_rate_min;
ALTER TABLE users DROP COLUMN IF EXISTS commission_rate_max;
ALTER TABLE users DROP COLUMN IF EXISTS pix_key;
ALTER TABLE users DROP COLUMN IF EXISTS salary_base;
ALTER TABLE users DROP COLUMN IF EXISTS whatsapp;
ALTER TABLE users DROP COLUMN IF EXISTS status;
