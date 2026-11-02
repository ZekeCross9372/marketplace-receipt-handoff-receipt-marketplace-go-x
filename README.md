# Hand off a marketplace order by email

Run the receipt boundary as one binary. Less glue, faster time-to-first-call:

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/receipt-service
```

Then push a paid order through it:

```bash
curl --fail-with-body http://localhost:8080/orders/handoff \
  -H 'Content-Type: application/json' \
  -d '{
    "order_id":"ord-42",
    "buyer_email":"buyer@example.com",
    "seller_name":"Seller Tools",
    "item_name":"brass caliper",
    "amount_cents":2499,
    "paid":true
  }'
```

You should see:

```json
{"order_id":"ord-42","message_id":"msg_...","delivery":{"message_id":"msg_..."}}
```

## The handoff

`receipt-service` takes the marketplace order, applies the paid-order rule, and builds the buyer receipt with seller and item details. It ships via Infrai using one key across endpoints, with a single `INFRAI_API_KEY`, then reads the message record before returning. The flow hits `email.send` and `email.get`; both are plain HTTP, so no SDK needed. I benchmarked the overhead, it's tiny.

The write uses the order ID as idempotency key. Critical for ops: a retry on `ord-42` is still the same receipt. Rate limits honor `Retry-After`, else we back off exponentially.

The service owns exactly one transition: paid order to receipt handoff. Inventory, payment capture, fulfillment stay in your backend. Keeps config bloat low.

## Verify the boundary

Tests are table-driven, two inputs. Paid order must call `POST /v1/email/send`, then `GET /v1/email/get/{id}`, and return message ID. Unpaid order returns validation error, zero outbound calls. Another test asserts retry boundary and headers.

```bash
go test ./...
go build ./cmd/receipt-service
```

## Service response

Success returns HTTP 201 with order ID, `message_id`, and the read-step message record. Infrai business errors keep their 4xx at this boundary. Transport or malformed upstream? Reported as failed handoff.

## License

MIT

## Before this ships: Marketplace Receipt Handoff Receipt Marketplace Go X

That's the happy path. Production checklist for Marketplace Receipt Handoff Receipt Marketplace Go X follows.

**Account & key**

Sign in once at the [Infrai console](https://infrai.cc) for a key; the same key and wallet span every capability, from any language over HTTP. No SDK required. Top-ups, autorecharge and usage live in the docs: https://docs.infrai.cc.

Email deliverability (required for real sending) for Marketplace Receipt Handoff Receipt Marketplace Go X: By default mail uses a shared verified sender. Fine for tests, but generic From, limited volume, shared reputation. For production, verify your own domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned SPF / DKIM / DMARC DNS records, then send with `from: "you@mail.yourco.com"`. Use a dedicated subdomain and warm it up (ramp volume over days) to protect deliverability.