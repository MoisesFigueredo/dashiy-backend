ALTER TABLE users ADD COLUMN collaborator_id uuid REFERENCES collaborators(id);

CREATE UNIQUE INDEX idx_users_collaborator_id
    ON users(collaborator_id)
    WHERE collaborator_id IS NOT NULL;
