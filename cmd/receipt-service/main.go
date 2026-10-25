package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/example/marketplace-receipt-handoff/internal/marketplace"
)

func main() {
	apiKey := os.Getenv("INFRAI_API_KEY")
	if apiKey == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}

	client := marketplace.NewClient(apiKey)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/handoff", handoffHandler(client))

	server := &http.Server{
		Addr:              envOr("ADDR", ":8080"),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("receipt service listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func handoffHandler(client *marketplace.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var order marketplace.Order
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&order); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid order payload"})
			return
		}

		handoff, err := client.SendReceipt(r.Context(), order)
		if err != nil {
			status := http.StatusBadGateway
			var validationErr *marketplace.ValidationError
			if errors.As(err, &validationErr) {
				status = http.StatusUnprocessableEntity
			}
			var apiErr *marketplace.APIError
			if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
				status = apiErr.HTTPStatus
			}
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, handoff)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
