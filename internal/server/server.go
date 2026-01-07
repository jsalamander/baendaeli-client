package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jsalamander/baendaeli-client/internal/actuator"
	"github.com/jsalamander/baendaeli-client/internal/config"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type Server struct {
	config     *config.Config
	httpClient *http.Client
}

type createPaymentPayload struct {
	AmountCents        int    `json:"amount_cents"`
	Currency           string `json:"currency"`
	PaymentRedirectURL string `json:"payment_redirect_url"`
}

func New(cfg *config.Config) *Server {
	return &Server{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (s *Server) Router() *chi.Mux {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/", s.handleIndex)
	r.Get("/ui.js", s.handleServeFile("ui.js"))
	r.Get("/api.js", s.handleServeFile("api.js"))
	r.Get("/qr.js", s.handleServeFile("qr.js"))
	r.Get("/main.js", s.handleServeFile("main.js"))
	r.Post("/api/payment", s.handleCreatePayment)
	r.Get("/api/payment/{id}", s.handleGetPaymentStatus)
	r.Post("/api/actuate", s.handleActuate)

	return r
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := indexPageData{
		DefaultAmount:    s.config.DefaultAmount,
		SuccessOverlayMs: s.config.SuccessOverlayMs,
	}
	if err := indexTemplate.Execute(w, data); err != nil {
		log.Printf("failed to render index template: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleServeFile(filename string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")

		// Only main.js needs template variable substitution
		if filename == "main.js" {
			data := indexPageData{
				DefaultAmount:    s.config.DefaultAmount,
				SuccessOverlayMs: s.config.SuccessOverlayMs,
			}
			if err := mainJS.Execute(w, data); err != nil {
				log.Printf("failed to serve %s: %v", filename, err)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
		} else {
			// Serve other files as plain static files
			content, err := GetStaticFile(filename)
			if err != nil {
				http.Error(w, "file not found", http.StatusNotFound)
				return
			}
			if _, err := w.Write(content); err != nil {
				log.Printf("failed to write %s: %v", filename, err)
			}
		}
	}
}

func (s *Server) handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	var payload createPaymentPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		// Allow empty body; fall back to defaults
		payload = createPaymentPayload{}
	}

	if payload.AmountCents == 0 {
		payload.AmountCents = s.config.DefaultAmount
	}
	if payload.Currency == "" {
		payload.Currency = "CHF"
	}
	if payload.PaymentRedirectURL == "" {
		payload.PaymentRedirectURL = "https://example.com/payments/123/complete"
	}

	reqBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to prepare payment request", http.StatusInternalServerError)
		return
	}

	targetURL := strings.TrimRight(s.config.BaendaeliURL, "/") + "/api/v1/payment"
	outbound, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		http.Error(w, "failed to create outbound request", http.StatusInternalServerError)
		return
	}
	outbound.Header.Set("Authorization", "Bearer "+s.config.BaendaeliAPIKey)
	outbound.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(outbound)
	if err != nil {
		http.Error(w, "failed to reach payment service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("failed to forward response: %v", err)
	}
}

func (s *Server) handleGetPaymentStatus(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "id")
	if paymentID == "" {
		http.Error(w, "payment id is required", http.StatusBadRequest)
		return
	}

	targetURL := strings.TrimRight(s.config.BaendaeliURL, "/") + "/api/v1/payment/" + paymentID
	outbound, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		http.Error(w, "failed to create outbound request", http.StatusInternalServerError)
		return
	}
	outbound.Header.Set("Authorization", "Bearer "+s.config.BaendaeliAPIKey)

	resp, err := s.httpClient.Do(outbound)
	if err != nil {
		http.Error(w, "failed to reach payment service", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("failed to forward status response: %v", err)
	}
}

func (s *Server) handleActuate(w http.ResponseWriter, r *http.Request) {
	totalMs, err := actuator.Trigger()
	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		log.Printf("actuator error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":        "error",
			"error":         "Solibändeli konnte nicht ausgegeben werden",
			"total_time_ms": 0,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":        "ok",
		"total_time_ms": totalMs,
	})
}
