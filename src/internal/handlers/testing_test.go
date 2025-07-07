package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/moehoshio/NekoLcServer/internal/models"
)

func TestTestingHandler_Ping(t *testing.T) {
	cfg := createTestConfig(false)
	
	handler := NewTestingHandler(cfg)
	
	req := httptest.NewRequest("GET", "/v0/testing/ping", nil)
	rr := httptest.NewRecorder()
	
	handler.Ping(rr, req)
	
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}
	
	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	if response["message"] != "pong" {
		t.Errorf("Expected message 'pong', got %v", response["message"])
	}
	
	if response["status"] != "ok" {
		t.Errorf("Expected status 'ok', got %v", response["status"])
	}
}

func TestTestingHandler_Echo(t *testing.T) {
	cfg := createTestConfig(false)
	cfg.App.Debug.Enabled = true
	
	handler := NewTestingHandler(cfg)
	
	testData := map[string]interface{}{
		"test": "hello world",
		"number": 42,
	}
	
	jsonData, _ := json.Marshal(testData)
	req := httptest.NewRequest("POST", "/v0/testing/echo", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Echo(rr, req)
	
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, rr.Code)
	}
	
	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	echo, ok := response["echo"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected echo object in response")
	}
	
	if echo["test"] != "hello world" {
		t.Errorf("Expected echo test 'hello world', got %v", echo["test"])
	}
}

func TestTestingHandler_Echo_InvalidJSON(t *testing.T) {
	cfg := createTestConfig(false)
	cfg.App.Debug.Enabled = true
	
	handler := NewTestingHandler(cfg)
	
	req := httptest.NewRequest("POST", "/v0/testing/echo", bytes.NewBufferString("{invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	
	handler.Echo(rr, req)
	
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, rr.Code)
	}
	
	var response models.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}
	
	if len(response.Errors) == 0 {
		t.Errorf("Expected at least one error in response")
	}
	
	if response.Errors[0].ErrorType != "InvalidRequest" {
		t.Errorf("Expected error type 'InvalidRequest', got %v", response.Errors[0].ErrorType)
	}
}