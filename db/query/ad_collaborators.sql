-- name: ListAdCollaboratorsByCompanyID :many
SELECT *
FROM ad_collaborators
WHERE company_id = $1
ORDER BY created_at DESC, role, user_id;

-- name: ListAdCollaboratorsByNicheID :many
SELECT *
FROM ad_collaborators
WHERE company_id = $1
  AND niche_id = $2
ORDER BY created_at DESC, role, user_id;
