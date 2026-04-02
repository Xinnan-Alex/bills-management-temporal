# Bills Management API

A bill management backend service built with [Encore.go](https://encore.dev) and [Temporal](https://temporal.io). Each bill is modeled as a durable Temporal workflow that accumulates line items over time and closes either manually or via a configurable auto-close timer.

## Technology Stack

| Layer | Technology | Purpose |
|---|---|---|
| **Framework** | Encore.go | API routing, database management, config, secrets, code generation |
| **Orchestration** | Temporal (Go SDK) | Durable workflow execution, signals, updates, timers |
| **Database** | PostgreSQL | Persistent storage for bills and line items |
| **Language** | Go 1.22+ | Application logic |

## How It Works

### Bill Lifecycle

```
CreateBill ──▶ Temporal Workflow starts (status: OPEN)
                    │
                    ├── AddLineItem signals ──▶ Persists to DB, updates in-memory state
                    │       (idempotent via dedup key)
                    │
                    ├── CloseBill update ──▶ Finalizes bill, returns invoice
                    │       OR
                    └── Auto-close timer fires ──▶ Finalizes bill automatically
```

1. **CreateBill** — Inserts a bill row in PostgreSQL and starts a Temporal workflow with a configurable close timeout (default: 60 minutes).
2. **AddLineItem** — Sends a Temporal signal to the running workflow. The workflow deduplicates by idempotency key in-memory, then calls an activity to persist the line item and update the running total.
3. **CloseBill** — Sends a Temporal update to the workflow. The update handler marks the bill as closed, calls `FinalizeBillActivity` to write the final total to the database, and synchronously returns the invoice with all line items.
4. **Auto-close** — If no manual close occurs within the configured timeout, the workflow's durable timer fires and finalizes the bill automatically.

### Idempotency

Line items use a client-provided `idempotencyKey`. Deduplication happens at two levels:
- **Workflow (in-memory)** — The `SeenItems` map skips duplicate signals without calling an activity.
- **Database (CTE)** — `INSERT ... ON CONFLICT DO NOTHING RETURNING id` ensures the running total is only incremented on actual inserts, not duplicates.

### Currency

Each bill is created with a currency (`GEL` or `USD`). All line items inherit the bill's currency — there is no per-item currency override. This prevents accidental cross-currency arithmetic.

## What Temporal Provides

Temporal orchestrates each bill as a long-running, durable workflow:

- **Durable Timer** — The auto-close timeout survives server restarts. No cron job or polling needed.
- **Signal Channel** — Line items are added via signals, which buffer automatically if the workflow is busy.
- **Update Handler** — `CloseBill` uses a Temporal update, which is synchronous — the caller blocks until the invoice is computed and returned.
- **In-Memory Dedup** — The workflow maintains a `SeenItems` map across its lifetime. Duplicate signals are rejected instantly without a database round-trip.
- **Automatic Retries** — Activities (DB writes) retry automatically on transient failures with configurable backoff.
- **Event History** — Every signal, activity, and timer is recorded in Temporal's event history for full auditability.

## With vs Without Temporal

| Aspect | With Temporal | Without Temporal (pure DB) |
|---|---|---|
| **Auto-close timer** | Durable timer — survives restarts, zero drift | Requires a cron job or scheduler polling the DB |
| **Close response** | Synchronous via Update — caller gets the invoice inline | Must poll or use a callback/webhook |
| **Idempotency** | Two layers: in-memory map (fast) + DB constraint (safe) | DB constraint only — every duplicate hits the database |
| **Signal ordering** | Temporal guarantees signal delivery order per workflow | Must handle concurrent requests with row-level locks |
| **Failure recovery** | Automatic activity retries with backoff | Manual retry logic or a separate job queue |
| **Auditability** | Full event history in Temporal UI | Must build your own audit log |
| **Operational complexity** | Requires running a Temporal cluster (or Temporal Cloud) | Simpler — just the app + database |
| **Latency** | Slight overhead per activity (task queue round-trip) | Direct DB calls, lower latency |
| **Testing** | `testsuite` with time-skipping, signal/update simulation | Standard integration tests |
| **Cost** | Additional infra (Temporal Cloud or self-hosted) | No extra services |

**When Temporal is a good fit:** Bills that stay open for hours/days, need reliable auto-close, real-time close responses, or heavy signal traffic with dedup.

**When pure DB is simpler:** Short-lived bills, infrequent closes, no auto-close requirement, or when operational simplicity is the priority.

## API Specification

Base URL: `http://localhost:4000` (local development)

### Create a Bill

```
POST /v1/bills
```

**Request:**
```json
{
  "currencyCode": "GEL"
}
```

**Response** `200 OK`:
```json
{
  "billId": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
}
```

**Validation:** `currencyCode` must be `"GEL"` or `"USD"`.

---

### Add a Line Item

```
POST /v1/bills/:billID/line-items
```

**Request:**
```json
{
  "description": "Coffee",
  "amountMinor": 350,
  "idempotencyKey": "order-123"
}
```

**Response:** `200 OK` (no body)

**Validation:**
- `amountMinor` must be positive
- `description` is required
- `idempotencyKey` is required

**Errors:**
- `409 FailedPrecondition` — Bill is closed

---

### Close a Bill

```
POST /v1/bills/:id/close
```

**Response** `200 OK`:
```json
{
  "billId": "a1b2c3d4-...",
  "currencyCode": "GEL",
  "totalAmountMinor": 1050,
  "lineItems": [
    {
      "id": "item-uuid",
      "description": "Coffee",
      "amountMinor": 350,
      "createdAt": "2026-04-03T12:00:00Z"
    }
  ],
  "closedAt": "2026-04-03T14:30:00Z"
}
```

**Errors:**
- `400 FailedPrecondition` — Bill is already closed

---

### Get a Bill

```
GET /v1/bills/:billID
```

**Response** `200 OK`:
```json
{
  "billId": "a1b2c3d4-...",
  "status": "OPEN",
  "currencyCode": "GEL",
  "totalMinor": 700,
  "lineItems": [
    {
      "id": "item-uuid",
      "description": "Coffee",
      "amountMinor": 350,
      "createdAt": "2026-04-03T12:00:00Z"
    }
  ],
  "createdAt": "2026-04-03T10:00:00Z",
  "updatedAt": "2026-04-03T12:00:00Z"
}
```

**Notes:**
- `totalMinor` shows `running_total_minor` for OPEN bills, `closed_total_minor` for CLOSED bills.
- `closedAt` is only present when status is `"CLOSED"`.

**Errors:**
- `404 NotFound` — Bill does not exist

---

### List Bills

```
GET /v1/bills?status=OPEN
```

**Query Parameters:**
- `status` (optional) — Filter by `"OPEN"` or `"CLOSED"`

**Response** `200 OK`:
```json
{
  "bills": [
    {
      "billId": "a1b2c3d4-...",
      "status": "OPEN",
      "currencyCode": "USD",
      "totalMinor": 1200,
      "createdAt": "2026-04-03T10:00:00Z",
      "updatedAt": "2026-04-03T12:00:00Z"
    }
  ]
}
```

## Project Structure

```
fees-api/
├── encore.app                          # Encore application config
├── go.mod
├── bills/
│   ├── bills.go                        # Service init, Temporal client/worker setup
│   ├── handlers.go                     # API endpoints and request/response types
│   ├── repository.go                   # Database operations and domain types
│   ├── activity.go                     # Temporal activity implementations
│   ├── config.go                       # Encore config struct and secrets
│   ├── config.cue                      # Environment-specific configuration
│   ├── handlers_test.go                # Request validation tests
│   ├── repository_test.go              # Database operation tests
│   ├── migrations/
│   │   ├── 1_create_bills_table.up.sql
│   │   └── 2_create_bill_line_items_table.up.sql
│   └── workflow/
│       ├── workflow.go                 # Temporal workflow definition
│       ├── types.go                    # Workflow types (signals, state, invoice)
│       └── workflow_test.go            # Workflow unit tests (testsuite)
```

## Running Locally

### Prerequisites

- [Encore CLI](https://encore.dev/docs/install)
- [Temporal CLI](https://docs.temporal.io/cli#install)
- Go 1.22+

### Start Services

```bash
# Terminal 1: Start Temporal dev server
temporal server start-dev

# Terminal 2: Start Encore app
encore run
``` 

The API is available at `http://localhost:4000`. Encore dashboard at `http://localhost:9400`.

### Run Tests

```bash
# All tests (requires Encore for DB tests)
encore test ./... -count=1

# Workflow tests only (pure Go, no Encore needed)
go test -v ./bills/workflow/ -count=1
```

## Configuration

| Key | Default | Description |
|---|---|---|
| `BillCloseTimeout` | `60` | Auto-close timer duration in minutes |
| `TemporalServer` | `localhost:7233` (local) | Temporal cluster address |
| `NameSpace` | `default` (local) | Temporal namespace |

Cloud values are configured in `bills/config.cue`. Secrets (`TemporalAPIKey`) are managed via `encore secret set`.
