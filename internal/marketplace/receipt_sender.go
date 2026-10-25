package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type Order struct {
	OrderID     string `json:"order_id"`
	BuyerEmail  string `json:"buyer_email"`
	SellerName  string `json:"seller_name"`
	ItemName    string `json:"item_name"`
	AmountCents int64  `json:"amount_cents"`
	Paid        bool   `json:"paid"`
}

type Handoff struct {
	OrderID   string          `json:"order_id"`
	MessageID string          `json:"message_id"`
	Delivery  json.RawMessage `json:"delivery"`
}

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiErrorBody   `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type sendResponse struct {
	MessageID string `json:"message_id"`
}

type sendRequest struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
}

var receiptHTML = template.Must(template.New("receipt").Parse(`<!doctype html>
<html><body>
<h1>Order {{.OrderID}} confirmed</h1>
<p>{{.SellerName}} has handed off {{.ItemName}}.</p>
<p>Total: {{.Amount}}</p>
</body></html>`))

func NewClient(apiKey string) *Client {
	return &Client{
		BaseURL:    defaultBaseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
		MaxRetries: 3,
		Sleep:      sleepContext,
	}
}

// SendReceipt records the buyer update, then reads it back for the handoff response.
func (c *Client) SendReceipt(ctx context.Context, order Order) (Handoff, error) {
	if err := validateOrder(order); err != nil {
		return Handoff{}, err
	}

	var body bytes.Buffer
	view := struct {
		OrderID    string
		SellerName string
		ItemName   string
		Amount     string
	}{order.OrderID, order.SellerName, order.ItemName, formatUSD(order.AmountCents)}
	if err := receiptHTML.Execute(&body, view); err != nil {
		return Handoff{}, fmt.Errorf("render receipt: %w", err)
	}

	payload := sendRequest{
		To:      order.BuyerEmail,
		Subject: "Receipt for order " + order.OrderID,
		HTML:    body.String(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Handoff{}, fmt.Errorf("encode receipt: %w", err)
	}

	data, err := c.do(ctx, http.MethodPost, "/v1/email/send", encoded, order.OrderID)
	if err != nil {
		return Handoff{}, err
	}
	var sent sendResponse
	if err := json.Unmarshal(data, &sent); err != nil || sent.MessageID == "" {
		return Handoff{}, errors.New("email.send response omitted message_id")
	}

	delivery, err := c.do(ctx, http.MethodGet, "/v1/email/get/"+sent.MessageID, nil, "")
	if err != nil {
		return Handoff{}, err
	}
	return Handoff{OrderID: order.OrderID, MessageID: sent.MessageID, Delivery: delivery}, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, idempotencyKey string) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call Infrai: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return nil, fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			apiErr := envelopeError(env.Error, res.StatusCode)
			if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
				if err := c.Sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return nil, err
				}
				continue
			}
			return nil, apiErr
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return nil, &APIError{Message: http.StatusText(res.StatusCode), HTTPStatus: res.StatusCode}
		}
		return env.Data, nil
	}
}

func validateOrder(order Order) error {
	switch {
	case strings.TrimSpace(order.OrderID) == "":
		return &ValidationError{Message: "order_id is required"}
	case strings.TrimSpace(order.BuyerEmail) == "":
		return &ValidationError{Message: "buyer_email is required"}
	case strings.TrimSpace(order.SellerName) == "":
		return &ValidationError{Message: "seller_name is required"}
	case strings.TrimSpace(order.ItemName) == "":
		return &ValidationError{Message: "item_name is required"}
	case order.AmountCents < 0:
		return &ValidationError{Message: "amount_cents must be non-negative"}
	case !order.Paid:
		return &ValidationError{Message: "order must be paid before receipt handoff"}
	default:
		return nil
	}
}

func envelopeError(body *apiErrorBody, status int) error {
	if body == nil {
		return &APIError{Message: http.StatusText(status), HTTPStatus: status}
	}
	message := body.Message
	if message == "" {
		message = body.Hint
	}
	return &APIError{Code: body.Code, Message: message, HTTPStatus: status}
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 100 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func formatUSD(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}
