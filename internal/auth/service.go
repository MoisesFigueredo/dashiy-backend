package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultTokenTTL = 72 * time.Hour

var (
	ErrConflict              = errors.New("resource already exists")
	ErrForbidden             = errors.New("forbidden")
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrNotFound              = errors.New("not found")
	ErrSystemBootstrapClosed = errors.New("system bootstrap already completed")
)

type Service struct {
	db        *pgxpool.Pool
	jwtSecret string
	tokenTTL  time.Duration
}

type BootstrapStatus struct {
	RequiresSetup bool `json:"requires_setup"`
}

type SessionResponse struct {
	Token       string              `json:"token"`
	SessionKind string              `json:"session_kind"`
	SystemUser  *SystemUserSession  `json:"system_user,omitempty"`
	Company     *CompanySession     `json:"company,omitempty"`
	User        *CompanyUserSession `json:"user,omitempty"`
}

type SystemUserSession struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type CompanySession struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Plan   string `json:"plan"`
	Active bool   `json:"active"`
}

type CompanyUserSession struct {
	ID          string     `json:"id"`
	CompanyID   string     `json:"company_id"`
	FullName    string     `json:"full_name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

type CompanySummary struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	LegalName  *string   `json:"legal_name,omitempty"`
	TaxID      *string   `json:"tax_id,omitempty"`
	Plan       string    `json:"plan"`
	Active     bool      `json:"active"`
	UsersCount int       `json:"users_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CompanyUserSummary struct {
	ID          string     `json:"id"`
	CompanyID   string     `json:"company_id"`
	Code        *string    `json:"code,omitempty"`
	FullName    string     `json:"full_name"`
	Email       string     `json:"email"`
	Role        string     `json:"role"`
	Active      bool       `json:"active"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateCompanyInput struct {
	Name          string
	Slug          string
	LegalName     string
	TaxID         string
	Plan          string
	AdminName     string
	AdminEmail    string
	AdminPassword string
}

type CreateCompanyResult struct {
	Company   CompanySummary     `json:"company"`
	AdminUser CompanyUserSummary `json:"admin_user"`
}

type CreateCompanyUserInput struct {
	Code     string
	FullName string
	Email    string
	Password string
	Role     string
}

func NewService(db *pgxpool.Pool, jwtSecret string) *Service {
	return &Service{
		db:        db,
		jwtSecret: jwtSecret,
		tokenTTL:  defaultTokenTTL,
	}
}

func (s *Service) BootstrapStatus(ctx context.Context) (BootstrapStatus, error) {
	var count int64
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM system_users`).Scan(&count); err != nil {
		return BootstrapStatus{}, fmt.Errorf("count system users: %w", err)
	}

	return BootstrapStatus{RequiresSetup: count == 0}, nil
}

func (s *Service) SetupInitialSystemUser(ctx context.Context, email string, password string, ipAddress string) (SessionResponse, error) {
	status, err := s.BootstrapStatus(ctx)
	if err != nil {
		return SessionResponse{}, err
	}
	if !status.RequiresSetup {
		return SessionResponse{}, ErrSystemBootstrapClosed
	}

	email = normalizeEmail(email)
	if email == "" {
		return SessionResponse{}, fmt.Errorf("email is required")
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return SessionResponse{}, err
	}
	totpSecret, err := GenerateTOTPSecret()
	if err != nil {
		return SessionResponse{}, err
	}

	var userID uuid.UUID
	if err := s.db.QueryRow(ctx, `
		INSERT INTO system_users (email, password_hash, totp_secret)
		VALUES ($1, $2, $3)
		RETURNING id
	`, email, passwordHash, totpSecret).Scan(&userID); err != nil {
		if isUniqueViolation(err) {
			return SessionResponse{}, ErrConflict
		}
		return SessionResponse{}, fmt.Errorf("create system user: %w", err)
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE system_users
		SET last_login_at = now(), last_login_ip = $2, updated_at = now()
		WHERE id = $1
	`, userID, emptyToNil(strings.TrimSpace(ipAddress))); err != nil {
		return SessionResponse{}, fmt.Errorf("update system user login: %w", err)
	}

	return s.issueSystemSession(userID, email)
}

func (s *Service) LoginSystem(ctx context.Context, email string, password string, ipAddress string) (SessionResponse, error) {
	email = normalizeEmail(email)
	if email == "" {
		return SessionResponse{}, ErrInvalidCredentials
	}

	var (
		userID       uuid.UUID
		passwordHash string
		active       bool
	)
	err := s.db.QueryRow(ctx, `
		SELECT id, password_hash, active
		FROM system_users
		WHERE lower(email) = lower($1)
		LIMIT 1
	`, email).Scan(&userID, &passwordHash, &active)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionResponse{}, ErrInvalidCredentials
		}
		return SessionResponse{}, fmt.Errorf("find system user: %w", err)
	}

	if !active || ComparePassword(passwordHash, password) != nil {
		return SessionResponse{}, ErrInvalidCredentials
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE system_users
		SET last_login_at = now(), last_login_ip = $2, updated_at = now()
		WHERE id = $1
	`, userID, emptyToNil(strings.TrimSpace(ipAddress))); err != nil {
		return SessionResponse{}, fmt.Errorf("update system user login: %w", err)
	}

	return s.issueSystemSession(userID, email)
}

func (s *Service) LoginCompanyUser(ctx context.Context, email string, password string) (SessionResponse, error) {
	email = normalizeEmail(email)
	if email == "" {
		return SessionResponse{}, ErrInvalidCredentials
	}

	type companyUserRecord struct {
		UserID        uuid.UUID
		CompanyID     uuid.UUID
		FullName      string
		Email         string
		PasswordHash  string
		Role          string
		UserActive    bool
		UserStatus    string
		CompanyName   string
		CompanyPlan   string
		CompanyActive bool
	}

	var record companyUserRecord
	err := s.db.QueryRow(ctx, `
		SELECT
			u.id,
			u.company_id,
			u.full_name,
			u.email,
			COALESCE(u.password_hash, ''),
			u.role,
			u.active,
			u.status,
			c.name,
			c.plan,
			c.active
		FROM users u
		INNER JOIN companies c
			ON c.id = u.company_id
		WHERE lower(u.email) = lower($1)
			AND c.deleted_at IS NULL
		LIMIT 1
	`, email).Scan(
		&record.UserID,
		&record.CompanyID,
		&record.FullName,
		&record.Email,
		&record.PasswordHash,
		&record.Role,
		&record.UserActive,
		&record.UserStatus,
		&record.CompanyName,
		&record.CompanyPlan,
		&record.CompanyActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SessionResponse{}, ErrInvalidCredentials
		}
		return SessionResponse{}, fmt.Errorf("find company user: %w", err)
	}

	if !record.CompanyActive || !record.UserActive || record.UserStatus != "active" || record.PasswordHash == "" || ComparePassword(record.PasswordHash, password) != nil {
		return SessionResponse{}, ErrInvalidCredentials
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE users
		SET last_login_at = now(), updated_at = now()
		WHERE id = $1
	`, record.UserID); err != nil {
		return SessionResponse{}, fmt.Errorf("update company user login: %w", err)
	}

	now := time.Now().UTC()
	claims := TokenClaims{
		Subject:     record.UserID.String(),
		SessionType: SessionTypeCompany,
		Email:       record.Email,
		FullName:    record.FullName,
		CompanyID:   record.CompanyID.String(),
		CompanyName: record.CompanyName,
		Role:        record.Role,
		Plan:        record.CompanyPlan,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(s.tokenTTL).Unix(),
	}

	token, err := SignToken(s.jwtSecret, claims)
	if err != nil {
		return SessionResponse{}, fmt.Errorf("sign company token: %w", err)
	}

	return SessionResponse{
		Token:       token,
		SessionKind: string(SessionTypeCompany),
		Company: &CompanySession{
			ID:     record.CompanyID.String(),
			Name:   record.CompanyName,
			Plan:   record.CompanyPlan,
			Active: record.CompanyActive,
		},
		User: &CompanyUserSession{
			ID:          record.UserID.String(),
			CompanyID:   record.CompanyID.String(),
			FullName:    record.FullName,
			Email:       record.Email,
			Role:        record.Role,
			Active:      record.UserActive,
			LastLoginAt: &now,
		},
	}, nil
}

func (s *Service) ChangeCompanyUserPassword(ctx context.Context, companyID uuid.UUID, userID uuid.UUID, currentPassword string, newPassword string) error {
	currentPassword = strings.TrimSpace(currentPassword)
	if currentPassword == "" {
		return fmt.Errorf("current password is required")
	}

	newPassword = strings.TrimSpace(newPassword)
	if newPassword == "" {
		return fmt.Errorf("new password is required")
	}

	var (
		passwordHash string
		active       bool
		status       string
	)
	err := s.db.QueryRow(ctx, `
		SELECT COALESCE(password_hash, ''), active, status
		FROM users
		WHERE id = $1
			AND company_id = $2
		LIMIT 1
	`, userID, companyID).Scan(&passwordHash, &active, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("find company user: %w", err)
	}

	if !active || status != "active" || passwordHash == "" || ComparePassword(passwordHash, currentPassword) != nil {
		return ErrInvalidCredentials
	}

	if ComparePassword(passwordHash, newPassword) == nil {
		return fmt.Errorf("new password must be different from current password")
	}

	nextHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(ctx, `
		UPDATE users
		SET password_hash = $3, updated_at = now()
		WHERE id = $1
			AND company_id = $2
	`, userID, companyID, nextHash); err != nil {
		return fmt.Errorf("update company user password: %w", err)
	}

	return nil
}

func (s *Service) AuthenticateToken(token string) (TokenClaims, error) {
	return ParseToken(s.jwtSecret, token)
}

func (s *Service) SessionFromClaims(claims TokenClaims) SessionResponse {
	switch claims.SessionType {
	case SessionTypeSystem:
		return SessionResponse{
			SessionKind: string(SessionTypeSystem),
			SystemUser: &SystemUserSession{
				ID:    claims.Subject,
				Email: claims.Email,
			},
		}
	case SessionTypeCompany:
		return SessionResponse{
			SessionKind: string(SessionTypeCompany),
			Company: &CompanySession{
				ID:     claims.CompanyID,
				Name:   claims.CompanyName,
				Plan:   claims.Plan,
				Active: true,
			},
			User: &CompanyUserSession{
				ID:        claims.Subject,
				CompanyID: claims.CompanyID,
				FullName:  claims.FullName,
				Email:     claims.Email,
				Role:      claims.Role,
				Active:    true,
			},
		}
	default:
		return SessionResponse{}
	}
}

func (s *Service) ListCompanies(ctx context.Context) ([]CompanySummary, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			c.id,
			c.name,
			c.slug,
			c.legal_name,
			c.tax_id,
			c.plan,
			c.active,
			c.created_at,
			c.updated_at,
			COUNT(u.id)::bigint AS users_count
		FROM companies c
		LEFT JOIN users u
			ON u.company_id = c.id
		WHERE c.deleted_at IS NULL
		GROUP BY c.id
		ORDER BY c.created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	defer rows.Close()

	items := make([]CompanySummary, 0)
	for rows.Next() {
		var (
			item       CompanySummary
			companyID  uuid.UUID
			legalName  pgtype.Text
			taxID      pgtype.Text
			usersCount int64
		)
		if err := rows.Scan(
			&companyID,
			&item.Name,
			&item.Slug,
			&legalName,
			&taxID,
			&item.Plan,
			&item.Active,
			&item.CreatedAt,
			&item.UpdatedAt,
			&usersCount,
		); err != nil {
			return nil, fmt.Errorf("scan company: %w", err)
		}

		item.ID = companyID.String()
		item.LegalName = textPointer(legalName)
		item.TaxID = textPointer(taxID)
		item.UsersCount = int(usersCount)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate companies: %w", err)
	}

	return items, nil
}

func (s *Service) CreateCompany(ctx context.Context, input CreateCompanyInput) (CreateCompanyResult, error) {
	if strings.TrimSpace(input.Name) == "" {
		return CreateCompanyResult{}, fmt.Errorf("company name is required")
	}
	if strings.TrimSpace(input.AdminName) == "" {
		return CreateCompanyResult{}, fmt.Errorf("admin name is required")
	}

	adminEmail := normalizeEmail(input.AdminEmail)
	if adminEmail == "" {
		return CreateCompanyResult{}, fmt.Errorf("admin email is required")
	}

	slug := normalizeSlug(firstNonEmpty(strings.TrimSpace(input.Slug), input.Name))
	if slug == "" {
		return CreateCompanyResult{}, fmt.Errorf("company slug is required")
	}

	passwordHash, err := HashPassword(input.AdminPassword)
	if err != nil {
		return CreateCompanyResult{}, err
	}

	plan := strings.TrimSpace(input.Plan)
	if plan == "" {
		plan = "starter"
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return CreateCompanyResult{}, fmt.Errorf("begin company transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var (
		company   CompanySummary
		adminUser CompanyUserSummary
	)
	var (
		companyID        uuid.UUID
		companyLegalName pgtype.Text
		companyTaxID     pgtype.Text
	)

	err = tx.QueryRow(ctx, `
		INSERT INTO companies (name, slug, legal_name, tax_id, plan)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, slug, legal_name, tax_id, plan, active, created_at, updated_at
	`,
		strings.TrimSpace(input.Name),
		slug,
		emptyToNil(strings.TrimSpace(input.LegalName)),
		emptyToNil(strings.TrimSpace(input.TaxID)),
		plan,
	).Scan(
		&companyID,
		&company.Name,
		&company.Slug,
		&companyLegalName,
		&companyTaxID,
		&company.Plan,
		&company.Active,
		&company.CreatedAt,
		&company.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateCompanyResult{}, ErrConflict
		}
		return CreateCompanyResult{}, fmt.Errorf("insert company: %w", err)
	}
	company.ID = companyID.String()
	company.LegalName = textPointer(companyLegalName)
	company.TaxID = textPointer(companyTaxID)

	var lastLoginAt pgtype.Timestamptz
	var adminUserID uuid.UUID
	var adminCompanyID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO users (company_id, code, full_name, email, password_hash, role)
		VALUES ($1, NULL, $2, $3, $4, 'admin')
		RETURNING id, company_id, code, full_name, email, role, active, last_login_at, created_at, updated_at
	`, companyID, strings.TrimSpace(input.AdminName), adminEmail, passwordHash).Scan(
		&adminUserID,
		&adminCompanyID,
		&adminUser.Code,
		&adminUser.FullName,
		&adminUser.Email,
		&adminUser.Role,
		&adminUser.Active,
		&lastLoginAt,
		&adminUser.CreatedAt,
		&adminUser.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return CreateCompanyResult{}, ErrConflict
		}
		return CreateCompanyResult{}, fmt.Errorf("insert admin user: %w", err)
	}
	adminUser.ID = adminUserID.String()
	adminUser.CompanyID = adminCompanyID.String()
	adminUser.LastLoginAt = timePointer(lastLoginAt)
	company.UsersCount = 1

	if err := tx.Commit(ctx); err != nil {
		return CreateCompanyResult{}, fmt.Errorf("commit company transaction: %w", err)
	}

	return CreateCompanyResult{
		Company:   company,
		AdminUser: adminUser,
	}, nil
}

func (s *Service) ListCompanyUsers(ctx context.Context, companyID uuid.UUID) ([]CompanyUserSummary, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, company_id, code, full_name, email, role, active, last_login_at, created_at, updated_at
		FROM users
		WHERE company_id = $1
		ORDER BY created_at DESC
	`, companyID)
	if err != nil {
		return nil, fmt.Errorf("list company users: %w", err)
	}
	defer rows.Close()

	items := make([]CompanyUserSummary, 0)
	for rows.Next() {
		var (
			item        CompanyUserSummary
			userID      uuid.UUID
			dbCompanyID uuid.UUID
			lastLoginAt pgtype.Timestamptz
		)
		if err := rows.Scan(
			&userID,
			&dbCompanyID,
			&item.Code,
			&item.FullName,
			&item.Email,
			&item.Role,
			&item.Active,
			&lastLoginAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan company user: %w", err)
		}

		item.ID = userID.String()
		item.CompanyID = dbCompanyID.String()
		item.LastLoginAt = timePointer(lastLoginAt)
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate company users: %w", err)
	}

	return items, nil
}

func (s *Service) CreateCompanyUser(ctx context.Context, companyID uuid.UUID, input CreateCompanyUserInput) (CompanyUserSummary, error) {
	if strings.TrimSpace(input.FullName) == "" {
		return CompanyUserSummary{}, fmt.Errorf("full name is required")
	}

	email := normalizeEmail(input.Email)
	if email == "" {
		return CompanyUserSummary{}, fmt.Errorf("email is required")
	}

	role := normalizeRole(input.Role)
	if !isAllowedRole(role) {
		return CompanyUserSummary{}, fmt.Errorf("invalid role")
	}

	passwordHash, err := HashPassword(input.Password)
	if err != nil {
		return CompanyUserSummary{}, err
	}

	var exists bool
	if err := s.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM companies
			WHERE id = $1
				AND deleted_at IS NULL
		)
	`, companyID).Scan(&exists); err != nil {
		return CompanyUserSummary{}, fmt.Errorf("check company: %w", err)
	}
	if !exists {
		return CompanyUserSummary{}, ErrNotFound
	}

	var (
		item        CompanyUserSummary
		userID      uuid.UUID
		dbCompanyID uuid.UUID
		lastLoginAt pgtype.Timestamptz
	)
	err = s.db.QueryRow(ctx, `
		INSERT INTO users (company_id, code, full_name, email, password_hash, role)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, company_id, code, full_name, email, role, active, last_login_at, created_at, updated_at
	`,
		companyID,
		emptyToNil(strings.TrimSpace(input.Code)),
		strings.TrimSpace(input.FullName),
		email,
		passwordHash,
		role,
	).Scan(
		&userID,
		&dbCompanyID,
		&item.Code,
		&item.FullName,
		&item.Email,
		&item.Role,
		&item.Active,
		&lastLoginAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return CompanyUserSummary{}, ErrConflict
		}
		return CompanyUserSummary{}, fmt.Errorf("create company user: %w", err)
	}

	item.ID = userID.String()
	item.CompanyID = dbCompanyID.String()
	item.LastLoginAt = timePointer(lastLoginAt)

	return item, nil
}

func (s *Service) issueSystemSession(userID uuid.UUID, email string) (SessionResponse, error) {
	now := time.Now().UTC()
	claims := TokenClaims{
		Subject:     userID.String(),
		SessionType: SessionTypeSystem,
		Email:       email,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(s.tokenTTL).Unix(),
	}

	token, err := SignToken(s.jwtSecret, claims)
	if err != nil {
		return SessionResponse{}, fmt.Errorf("sign system token: %w", err)
	}

	return SessionResponse{
		Token:       token,
		SessionKind: string(SessionTypeSystem),
		SystemUser: &SystemUserSession{
			ID:    userID.String(),
			Email: email,
		},
	}, nil
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

func normalizeSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer(
		"\u00e1", "a", "\u00e0", "a", "\u00e2", "a", "\u00e3", "a", "\u00e4", "a",
		"\u00e9", "e", "\u00e8", "e", "\u00ea", "e", "\u00eb", "e",
		"\u00ed", "i", "\u00ec", "i", "\u00ee", "i", "\u00ef", "i",
		"\u00f3", "o", "\u00f2", "o", "\u00f4", "o", "\u00f5", "o", "\u00f6", "o",
		"\u00fa", "u", "\u00f9", "u", "\u00fb", "u", "\u00fc", "u",
		"\u00e7", "c", "\u00f1", "n",
	).Replace(value)
	value = nonSlugChars.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func normalizeRole(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isAllowedRole(value string) bool {
	switch value {
	case "admin", "gestor_trafego", "traffic_manager", "copywriter", "editor", "closer", "gestor_projetos", "analyst":
		return true
	default:
		return false
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func timePointer(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	result := value.Time.UTC()
	return &result
}

func textPointer(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}

	result := value.String
	return &result
}

func emptyToNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
