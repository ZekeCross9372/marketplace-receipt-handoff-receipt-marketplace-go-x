package marketplace

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendReceiptBusinessDecision(t *testing.T) {
	tests := []struct {
		name      string
		paid      bool
		wantCalls int
		wantErr   string
	}{
		{name: "paid order sends and reads buyer update", paid: true, wantCalls: 2},
		{name: "unpaid order stays local", paid: false, wantCalls: 0, wantErr: "order must be paid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var mu sync.Mutex
			var calls int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()

				if r.Header.Get("Authorization") != "Bearer test-key" {
					t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
				}
				switch r.URL.Path {
				case "/v1/email/send":
					if r.Method != http.MethodPost {
						t.Errorf("send method = %s", r.Method)
					}
					if r.Header.Get("Idempotency-Key") != "ord-42" {
						t.Errorf("Idempotency-Key = %q", r.Header.Get("Idempotency-Key"))
					}
					var payload map[string]any
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Fatal(err)
					}
					if len(payload) != 3 || payload["to"] != "buyer@example.com" || !strings.Contains(payload["html"].(string), "Seller Tools") {
						t.Fatalf("send payload = %#v", payload)
					}
					writeTestEnvelope(w, http.StatusOK, true, map[string]string{"message_id": "msg-7"}, nil)
				case "/v1/email/get/msg-7":
					if r.Method != http.MethodGet {
						t.Errorf("get method = %s", r.Method)
					}
					writeTestEnvelope(w, http.StatusOK, true, map[string]string{"message_id": "msg-7"}, nil)
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer server.Close()

			client := NewClient("test-key")
			client.BaseURL = server.URL
			client.HTTPClient = server.Client()
			_, err := client.SendReceipt(context.Background(), Order{
				OrderID: "ord-42", BuyerEmail: "buyer@example.com", SellerName: "Seller Tools",
				ItemName: "brass caliper", AmountCents: 2499, Paid: tt.paid,
			})
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
			}
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestSendReceiptRetriesRateLimitAfterEnvelopeDecode(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			writeTestEnvelope(w, http.StatusTooManyRequests, false, nil, map[string]string{"message": "retry later"})
			return
		}
		if r.URL.Path == "/v1/email/send" {
			writeTestEnvelope(w, http.StatusOK, true, map[string]string{"message_id": "msg-8"}, nil)
			return
		}
		writeTestEnvelope(w, http.StatusOK, true, map[string]string{"message_id": "msg-8"}, nil)
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTPClient = server.Client()
	client.Sleep = func(context.Context, time.Duration) error { return nil }
	_, err := client.SendReceipt(context.Background(), Order{
		OrderID: "ord-43", BuyerEmail: "buyer@example.com", SellerName: "Bench Supply",
		ItemName: "steel ruler", AmountCents: 1200, Paid: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func writeTestEnvelope(w http.ResponseWriter, status int, ok bool, data any, apiErr any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "data": data, "error": apiErr, "metadata": map[string]any{}})
}
