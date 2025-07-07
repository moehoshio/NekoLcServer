package models

// Authentication models

type LoginRequest struct {
	Auth        AuthInfo    `json:"auth"`
	Preferences Preferences `json:"preferences,omitempty"`
}

type AuthInfo struct {
	// Username/password authentication
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	
	// Identifier/signature authentication
	Identifier string `json:"identifier,omitempty"`
	Timestamp  int64  `json:"timestamp,omitempty"`
	Signature  string `json:"signature,omitempty"`
}

type LoginResponse struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	Meta         Meta   `json:"meta"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type RefreshResponse struct {
	AccessToken string `json:"accessToken"`
	Meta        Meta   `json:"meta"`
}

type ValidateRequest struct {
	AccessToken string `json:"accessToken"`
}

type LogoutRequest struct {
	Logout LogoutInfo `json:"logout"`
}

type LogoutInfo struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}