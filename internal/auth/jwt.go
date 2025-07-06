package auth

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type JWTAuth struct {
	secretKey []byte
}

type Claims struct {
	UserID    string `json:"user_id"`
	Timestamp int64  `json:"timestamp"`
	TokenType string `json:"token_type"` // "access" or "refresh"
	jwt.RegisteredClaims
}

func NewJWTAuth(secretKey string) *JWTAuth {
	return &JWTAuth{
		secretKey: []byte(secretKey),
	}
}

// GenerateTokensFromCredentials generates JWT tokens from username/password
func (j *JWTAuth) GenerateTokensFromCredentials(username, password string) (string, string, error) {
	// In a real implementation, you would validate credentials against a database
	if username != "admin" || password != "password" {
		return "", "", fmt.Errorf("invalid credentials")
	}
	
	userID := username
	return j.generateTokenPair(userID)
}

// GenerateTokensFromSignature generates JWT tokens from identifier + timestamp + signature
func (j *JWTAuth) GenerateTokensFromSignature(identifier string, timestamp int64, signature string) (string, string, error) {
	// Validate signature - it should be SHA256 hash of identifier + timestamp + secret
	expectedSignature := j.generateSignature(identifier, timestamp)
	if signature != expectedSignature {
		return "", "", fmt.Errorf("invalid signature")
	}
	
	// Check timestamp is recent (within 5 minutes)
	now := time.Now().Unix()
	if abs(now-timestamp) > 300 { // 5 minutes
		return "", "", fmt.Errorf("timestamp too old or in future")
	}
	
	userID := identifier
	return j.generateTokenPair(userID)
}

func (j *JWTAuth) generateTokenPair(userID string) (string, string, error) {
	now := time.Now()
	
	// Generate access token (1 hour expiry)
	accessClaims := Claims{
		UserID:    userID,
		Timestamp: now.Unix(),
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "NekoLcServer",
		},
	}
	
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString(j.secretKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign access token: %w", err)
	}
	
	// Generate refresh token (30 days expiry)
	refreshClaims := Claims{
		UserID:    userID,
		Timestamp: now.Unix(),
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour * 24 * 30)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "NekoLcServer",
		},
	}
	
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(j.secretKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to sign refresh token: %w", err)
	}
	
	return accessTokenString, refreshTokenString, nil
}

// ValidateToken validates a JWT token and returns claims if valid
func (j *JWTAuth) ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secretKey, nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}
	
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	
	return nil, fmt.Errorf("invalid token")
}

// RefreshAccessToken generates a new access token from a valid refresh token
func (j *JWTAuth) RefreshAccessToken(refreshTokenString string) (string, error) {
	claims, err := j.ValidateToken(refreshTokenString)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token: %w", err)
	}
	
	if claims.TokenType != "refresh" {
		return "", fmt.Errorf("token is not a refresh token")
	}
	
	// Generate new access token
	now := time.Now()
	accessClaims := Claims{
		UserID:    claims.UserID,
		Timestamp: now.Unix(),
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "NekoLcServer",
		},
	}
	
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	return accessToken.SignedString(j.secretKey)
}

// generateSignature creates expected signature for ID + timestamp authentication
func (j *JWTAuth) generateSignature(identifier string, timestamp int64) string {
	data := identifier + strconv.FormatInt(timestamp, 10) + string(j.secretKey)
	hash := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", hash)
}

// GetTokenHash returns a hash of the token for storage
func (j *JWTAuth) GetTokenHash(tokenString string) string {
	hash := sha256.Sum256([]byte(tokenString))
	return fmt.Sprintf("%x", hash)
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}