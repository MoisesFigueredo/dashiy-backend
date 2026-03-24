package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type SessionType string

const (
	SessionTypeSystem  SessionType = "system"
	SessionTypeCompany SessionType = "company"
)

type TokenClaims struct {
	Subject     string      `json:"sub"`
	SessionType SessionType `json:"session_type"`
	Email       string      `json:"email"`
	FullName    string      `json:"full_name,omitempty"`
	CompanyID   string      `json:"company_id,omitempty"`
	CompanyName string      `json:"company_name,omitempty"`
	CompanySlug string      `json:"company_slug,omitempty"`
	Role        string      `json:"role,omitempty"`
	Plan        string      `json:"plan,omitempty"`
	IssuedAt    int64       `json:"iat"`
	ExpiresAt   int64       `json:"exp"`
}

var tokenHeader = []byte(`{"alg":"HS256","typ":"JWT"}`)

func SignToken(secret string, claims TokenClaims) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", fmt.Errorf("jwt secret is empty")
	}

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal token claims: %w", err)
	}

	headerEncoded := encodeTokenPart(tokenHeader)
	payloadEncoded := encodeTokenPart(payload)
	signingInput := headerEncoded + "." + payloadEncoded
	signature := signToken(secret, signingInput)

	return signingInput + "." + encodeTokenPart(signature), nil
}

func ParseToken(secret string, token string) (TokenClaims, error) {
	if strings.TrimSpace(secret) == "" {
		return TokenClaims{}, fmt.Errorf("jwt secret is empty")
	}

	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return TokenClaims{}, fmt.Errorf("invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	expectedSignature := signToken(secret, signingInput)

	receivedSignature, err := decodeTokenPart(parts[2])
	if err != nil {
		return TokenClaims{}, fmt.Errorf("decode token signature: %w", err)
	}
	if !hmac.Equal(receivedSignature, expectedSignature) {
		return TokenClaims{}, fmt.Errorf("invalid token signature")
	}

	payload, err := decodeTokenPart(parts[1])
	if err != nil {
		return TokenClaims{}, fmt.Errorf("decode token payload: %w", err)
	}

	var claims TokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return TokenClaims{}, fmt.Errorf("unmarshal token claims: %w", err)
	}

	if claims.ExpiresAt <= time.Now().Unix() {
		return TokenClaims{}, fmt.Errorf("token expired")
	}

	return claims, nil
}

func ExtractBearerToken(headerValue string) string {
	headerValue = strings.TrimSpace(headerValue)
	if headerValue == "" {
		return ""
	}

	const prefix = "bearer "
	if len(headerValue) < len(prefix) || strings.ToLower(headerValue[:len(prefix)]) != prefix {
		return ""
	}

	return strings.TrimSpace(headerValue[len(prefix):])
}

func encodeTokenPart(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeTokenPart(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}

func signToken(secret string, signingInput string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return mac.Sum(nil)
}
