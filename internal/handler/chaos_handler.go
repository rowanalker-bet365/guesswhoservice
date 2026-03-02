package handler

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"

	"github.com/guesswho/internal/logging"
	"github.com/guesswho/internal/service"
)

// ChaosHandler handles the chaos trigger endpoint.
type ChaosHandler struct {
	chaosService service.ChaosService
	apiKey       string
}

// NewChaosHandler creates a new ChaosHandler.
func NewChaosHandler(chaosService service.ChaosService, apiKey string) *ChaosHandler {
	return &ChaosHandler{chaosService: chaosService, apiKey: apiKey}
}

// authorize checks the X-Key-Id header against the configured API key.
func (h *ChaosHandler) authorize(w http.ResponseWriter, r *http.Request) bool {
	if h.apiKey == "" {
		logging.Warn(r.Context(), "chaos trigger accessed but API key not configured")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "chaos trigger is not configured"})
		return false
	}
	provided := r.Header.Get("X-Key-Id")
	if subtle.ConstantTimeCompare([]byte(provided), []byte(h.apiKey)) != 1 {
		logging.Warn(r.Context(), "unauthorized chaos trigger attempt")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "unauthorized",
			"message": "Invalid or missing X-Key-Id header.",
		})
		return false
	}
	return true
}

// TriggerChaos handles POST /chaos/trigger
func (h *ChaosHandler) TriggerChaos(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	var req struct {
		Enabled         bool `json:"enabled"`
		WindowSeconds   int  `json:"windowSeconds,omitempty"`
		IntervalSeconds int  `json:"intervalSeconds,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":   "invalid_request_body",
			"message": "Expected JSON body: {\"enabled\": true}",
		})
		return
	}

	if req.Enabled {
		h.chaosService.Enable()
		logging.Info(r.Context(), "chaos mode ACTIVATED via trigger")
	} else {
		h.chaosService.Disable()
		logging.Info(r.Context(), "chaos mode DEACTIVATED via trigger")
	}

	if req.WindowSeconds > 0 || req.IntervalSeconds > 0 {
		h.chaosService.SetWindowConfig(req.WindowSeconds, req.IntervalSeconds)
		logging.Info(r.Context(), "chaos window config updated",
			"windowSeconds", req.WindowSeconds,
			"intervalSeconds", req.IntervalSeconds,
		)
	}

	windowSeconds, intervalSeconds := h.chaosService.GetWindowConfig()

	message := "Chaos mode deactivated. Session APIs will respond normally."
	if req.Enabled {
		message = "Chaos mode activated. Session APIs will experience intermittent failures during chaos windows."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chaosEnabled":    req.Enabled,
		"message":         message,
		"windowSeconds":   windowSeconds,
		"intervalSeconds": intervalSeconds,
	})
}

// GetChaosStatus handles GET /chaos/status
func (h *ChaosHandler) GetChaosStatus(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(w, r) {
		return
	}

	windowSeconds, intervalSeconds := h.chaosService.GetWindowConfig()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chaosEnabled":    h.chaosService.IsEnabled(),
		"windowSeconds":   windowSeconds,
		"intervalSeconds": intervalSeconds,
	})
}
