package auth

import (
	"testing"
	"time"
)

func TestJWTAuth_GenerateAndValidateTokens(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")
	
	// Test username/password authentication
	accessToken, refreshToken, err := jwtAuth.GenerateTokensFromCredentials("admin", "password")
	if err != nil {
		t.Fatalf("Failed to generate tokens: %v", err)
	}
	
	// Validate access token
	claims, err := jwtAuth.ValidateToken(accessToken)
	if err != nil {
		t.Fatalf("Failed to validate access token: %v", err)
	}
	
	if claims.UserID != "admin" {
		t.Errorf("Expected user ID 'admin', got %s", claims.UserID)
	}
	
	if claims.TokenType != "access" {
		t.Errorf("Expected token type 'access', got %s", claims.TokenType)
	}
	
	// Validate refresh token
	refreshClaims, err := jwtAuth.ValidateToken(refreshToken)
	if err != nil {
		t.Fatalf("Failed to validate refresh token: %v", err)
	}
	
	if refreshClaims.TokenType != "refresh" {
		t.Errorf("Expected token type 'refresh', got %s", refreshClaims.TokenType)
	}
}

func TestJWTAuth_RefreshToken(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")
	
	// Generate initial tokens
	_, refreshToken, err := jwtAuth.GenerateTokensFromCredentials("admin", "password")
	if err != nil {
		t.Fatalf("Failed to generate tokens: %v", err)
	}
	
	// Refresh access token
	newAccessToken, err := jwtAuth.RefreshAccessToken(refreshToken)
	if err != nil {
		t.Fatalf("Failed to refresh access token: %v", err)
	}
	
	// Validate new access token
	claims, err := jwtAuth.ValidateToken(newAccessToken)
	if err != nil {
		t.Fatalf("Failed to validate new access token: %v", err)
	}
	
	if claims.TokenType != "access" {
		t.Errorf("Expected token type 'access', got %s", claims.TokenType)
	}
}

func TestJWTAuth_SignatureAuth(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")
	
	identifier := "test-device"
	timestamp := time.Now().Unix()
	
	// Generate the correct signature
	expectedSignature := jwtAuth.generateSignature(identifier, timestamp)
	
	// Test with correct signature
	accessToken, refreshToken, err := jwtAuth.GenerateTokensFromSignature(identifier, timestamp, expectedSignature)
	if err != nil {
		t.Fatalf("Failed to generate tokens with signature: %v", err)
	}
	
	// Validate tokens
	claims, err := jwtAuth.ValidateToken(accessToken)
	if err != nil {
		t.Fatalf("Failed to validate access token: %v", err)
	}
	
	if claims.UserID != identifier {
		t.Errorf("Expected user ID '%s', got %s", identifier, claims.UserID)
	}
	
	_, err = jwtAuth.ValidateToken(refreshToken)
	if err != nil {
		t.Fatalf("Failed to validate refresh token: %v", err)
	}
}

func TestJWTAuth_InvalidSignature(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")
	
	identifier := "test-device"
	timestamp := time.Now().Unix()
	invalidSignature := "invalid-signature"
	
	// Test with invalid signature
	_, _, err := jwtAuth.GenerateTokensFromSignature(identifier, timestamp, invalidSignature)
	if err == nil {
		t.Error("Expected error with invalid signature")
	}
}

func TestJWTAuth_ExpiredTimestamp(t *testing.T) {
	jwtAuth := NewJWTAuth("test-secret")
	
	identifier := "test-device"
	timestamp := time.Now().Unix() - 600 // 10 minutes ago
	signature := jwtAuth.generateSignature(identifier, timestamp)
	
	// Test with expired timestamp
	_, _, err := jwtAuth.GenerateTokensFromSignature(identifier, timestamp, signature)
	if err == nil {
		t.Error("Expected error with expired timestamp")
	}
}