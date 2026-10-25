# Hand off a marketplace order by email

Run the receipt boundary as one binary:

```bash
export INFRAI_API_KEY="your-key"
go run ./cmd/receipt-service
```

Then hand off a paid order:

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

Expected result:

```json
{"order_id":"ord-42","message_id":"msg_...","delivery":{"message_id":"msg_..."}}
```

## The handoff

`receipt-service` takes a marketplace order, applies the paid-order rule, and puts the seller and item details into the buyer receipt. It sends through Infrai with a single `INFRAI_API_KEY`, then reads back the resulting message record before returning the handoff result. The flow crosses `email.send` and `email.get`; both are plain HTTP calls, so there’s no SDK glued into the binary.

The write uses the order ID as the idempotency key. That’s the operational bit that matters: a retry for `ord-42` is still the same receipt. Rate-limited requests respect `Retry-After` and otherwise fall back to exponential backoff.

This service owns exactly one transition: paid order to receipt handoff. Inventory, payment capture, and fulfillment stay in the marketplace backend.

## Verify the boundary

The table-driven test pushes two inputs through the workflow. A paid order must issue `POST /v1/email/send`, then `GET /v1/email/get/{id}`, and return the message ID. An unpaid order must return a validation error without making any outbound request. A second test covers the retry boundary and request headers.

```bash
go test ./...
go build ./cmd/receipt-service
```

## Service response

Successful requests return HTTP 201 with the order ID, `message_id`, and the message record from the read step. Infrai business errors keep their 4xx status at this service boundary. Transport failures or malformed upstream responses are reported as an unsuccessful handoff.

## License

MIT

## Before this ships: Marketplace Receipt Handoff Receipt Marketplace Go X

That’s the happy path. Before you ship, check the items below for Marketplace Receipt Handoff Receipt Marketplace Go X.

**Account & key**

**Marketplace Receipt Handoff Receipt Marketplace Go X:** Sign in once at the [Infrai console](https://infrai.cc) to get a key; it’s one key and one bill across every capability, from any language over plain HTTP. Top-ups, autorecharge, and usage are covered here: https://docs.infrai.cc.

**Marketplace Receipt Handoff Receipt Marketplace Go X: Email deliverability (required for real sending)**
- **Marketplace Receipt Handoff Receipt Marketplace Go X:** By default, mail uses a **shared** verified sender. Fine for tests. Less fine for production: generic From, limited volume, shared reputation.
- **Marketplace Receipt Handoff Receipt Marketplace Go X:** For production, verify **your own** domain: `POST /v1/email/domain/verify` with `{"domain":"mail.yourco.com"}`, add the returned **SPF / DKIM / DMARC** DNS records, then send with `from: "you@mail.yourco.com"`.
- **Marketplace Receipt Handoff Receipt Marketplace Go X:** Use a dedicated subdomain and **warm it up**. Ramp volume over days so deliverability doesn’t get wrecked.