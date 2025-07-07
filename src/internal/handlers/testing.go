package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/moehoshio/NekoLcServer/internal/config"
	"github.com/moehoshio/NekoLcServer/internal/middleware"
	"github.com/moehoshio/NekoLcServer/internal/models"
)

type TestingHandler struct {
	Config *config.Config
}

func NewTestingHandler(cfg *config.Config) *TestingHandler {
	return &TestingHandler{Config: cfg}
}

// Ping handles GET /v0/testing/ping
func (h *TestingHandler) Ping(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	response := map[string]interface{}{
		"message": "pong",
		"status":  "ok",
		"meta":    models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}

// Echo handles POST /v0/testing/echo (debug only)
func (h *TestingHandler) Echo(w http.ResponseWriter, r *http.Request) {
	rw := &middleware.ResponseWriter{
		ResponseWriter: w,
		Config:         h.Config,
	}
	
	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Failed to read request body")
		return
	}
	defer r.Body.Close()
	
	// Try to parse as JSON to validate format
	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		rw.WriteError(http.StatusBadRequest, "InvalidRequest", "Invalid JSON format")
		return
	}
	
	// Create response with echo data and meta
	response := map[string]interface{}{
		"echo": jsonData,
		"meta": models.NewMeta(h.Config.App.Server.APIVersion, h.Config.App.Server.MinAPIVersion, h.Config.App.Server.BuildVersion, h.Config.App.Server.ReleaseDate),
	}
	
	rw.WriteJSON(http.StatusOK, response)
}