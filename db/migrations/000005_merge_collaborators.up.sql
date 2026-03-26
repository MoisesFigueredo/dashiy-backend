-- 1. Add collaborator-specific columns to users table
ALTER TABLE users
    ADD COLUMN commission_rate_min numeric(8,4) NOT NULL DEFAULT 0,
    ADD COLUMN commission_rate_max numeric(8,4) NOT NULL DEFAULT 0,
    ADD COLUMN pix_key text,
    ADD COLUMN salary_base numeric(16,2) NOT NULL DEFAULT 0,
    ADD COLUMN whatsapp text,
    ADD COLUMN status text NOT NULL DEFAULT 'active';

ALTER TABLE users
    ADD CONSTRAINT users_status_check CHECK (status IN ('active', 'inactive')),
    ADD CONSTRAINT users_commission_rate_min_check CHECK (commission_rate_min >= 0),
    ADD CONSTRAINT users_commission_rate_max_check CHECK (commission_rate_max >= commission_rate_min);

-- 2. Migrate collaborator data into linked users
UPDATE users u
SET
    commission_rate_min = c.commission_rate_min,
    commission_rate_max = c.commission_rate_max,
    pix_key = c.pix_key,
    salary_base = c.salary_base,
    whatsapp = c.whatsapp,
    status = c.status
FROM collaborators c
WHERE u.collaborator_id = c.id;

-- 3. Migrate ad_collaborators: set user_id from collaborator_id via users.collaborator_id
UPDATE ad_collaborators ac
SET user_id = u.id
FROM users u
WHERE u.collaborator_id = ac.collaborator_id
  AND ac.collaborator_id IS NOT NULL
  AND ac.user_id IS NULL;

-- 4. Delete orphan ad_collaborators that have no user_id (collaborator without linked user)
DELETE FROM ad_collaborators WHERE user_id IS NULL;

-- 5. Migrate commission_entries: set user_id from collaborator_id
UPDATE commission_entries ce
SET user_id = u.id
FROM users u
WHERE u.collaborator_id = ce.collaborator_id
  AND ce.collaborator_id IS NOT NULL
  AND ce.user_id IS NULL;

-- 6. Delete orphan commission_entries that have no user_id
DELETE FROM commission_entries WHERE user_id IS NULL AND collaborator_id IS NOT NULL;

-- 7. Drop collaborator-related indexes and columns from ad_collaborators
DROP INDEX IF EXISTS uq_ad_collaborators_collaborator;
DROP INDEX IF EXISTS idx_ad_collaborators_collaborator_id;
ALTER TABLE ad_collaborators DROP COLUMN collaborator_id;
ALTER TABLE ad_collaborators ALTER COLUMN user_id SET NOT NULL;

-- 8. Drop collaborator-related indexes and columns from commission_entries
DROP INDEX IF EXISTS uq_commission_entries_ad_snapshot;
DROP INDEX IF EXISTS idx_commission_entries_collaborator_month;
ALTER TABLE commission_entries DROP COLUMN collaborator_id;

-- Recreate the unique index for ad_snapshot commission entries using user_id
CREATE UNIQUE INDEX uq_commission_entries_ad_snapshot
    ON commission_entries (ad_id, snapshot_date, user_id, role, source_type)
    WHERE source_type = 'ad_snapshot'
      AND ad_id IS NOT NULL
      AND snapshot_date IS NOT NULL
      AND user_id IS NOT NULL;

CREATE INDEX idx_commission_entries_user_month
    ON commission_entries (user_id, snapshot_date DESC)
    WHERE user_id IS NOT NULL;

-- 9. Drop collaborator_id from users (no longer needed as bridge)
DROP INDEX IF EXISTS idx_users_collaborator_id;
ALTER TABLE users DROP COLUMN collaborator_id;

-- 10. Drop collaborators table
DROP TABLE collaborators;

-- 11. Make email globally unique (remove company-scoped uniqueness)
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_company_id_email_key;
ALTER TABLE users ADD CONSTRAINT users_email_unique UNIQUE (email);

-- 12. Add index for collaborator-role queries
CREATE INDEX idx_users_company_role_status ON users (company_id, role, status);
