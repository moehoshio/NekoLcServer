package models

import (
	"time"
)

// Meta represents API meta information included in every response
type Meta struct {
	APIVersion        string `json:"apiVersion"`
	MinAPIVersion     string `json:"minApiVersion"`
	BuildVersion      string `json:"buildVersion"`
	Timestamp         int64  `json:"timestamp"`
	ReleaseDate       string `json:"releaseDate"`
	Deprecated        bool   `json:"deprecated"`
	DeprecatedMessage string `json:"deprecatedMessage"`
}

// Preferences represents user preferences
type Preferences struct {
	Language string `json:"language,omitempty"`
}

// ErrorInfo represents a single error in the standard error response format
type ErrorInfo struct {
	Error        string `json:"error"`
	ErrorType    string `json:"errorType"`
	ErrorMessage string `json:"errorMessage"`
}

// ErrorResponse represents the standard error response format
type ErrorResponse struct {
	Errors []ErrorInfo `json:"errors"`
	Meta   Meta        `json:"meta"`
}

// BaseResponse represents a response with meta information
type BaseResponse struct {
	Meta Meta `json:"meta"`
}

// NewMeta creates a new Meta struct with current timestamp
func NewMeta(apiVersion, minAPIVersion, buildVersion, releaseDate string) Meta {
	return Meta{
		APIVersion:        apiVersion,
		MinAPIVersion:     minAPIVersion,
		BuildVersion:      buildVersion,
		Timestamp:         time.Now().Unix(),
		ReleaseDate:       releaseDate,
		Deprecated:        false,
		DeprecatedMessage: "",
	}
}

// NewErrorResponse creates a standard error response
func NewErrorResponse(meta Meta, errorType, errorMessage string) ErrorResponse {
	var errorClass string
	switch errorType {
	case "InvalidRequest", "NotFound", "Unauthorized":
		errorClass = "ForClientError"
	default:
		errorClass = "ForServerError"
	}
	
	return ErrorResponse{
		Errors: []ErrorInfo{
			{
				Error:        errorClass,
				ErrorType:    errorType,
				ErrorMessage: errorMessage,
			},
		},
		Meta: meta,
	}
}