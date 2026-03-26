DROP INDEX IF EXISTS idx_users_collaborator_id;

ALTER TABLE users DROP COLUMN IF EXISTS collaborator_id;
