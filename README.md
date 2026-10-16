# Doorstep Service Booking

A booking service for doorstep vehicle-battery services. A customer picks a service, a pincode and a time slot. The system assigns a free technician who has the right skill and covers that zone, and never double-books anyone, even under concurrent requests.

**Stack:** Go 1.26 · Gin · PostgreSQL 16 · Redis 7 · pgx v5 · golang-migrate · testcontainers-go

---

## Quick start

```bash
docker compose up --build
```

This starts the following, in order:

| Service    | What it does                                                         |
|------------|----------------------------------------------------------------------|
| `postgres` | Database (exposed on `localhost:5432`, user/pass/db `booking`)       |
| `redis`    | Technician locks and rate-limit counters (`localhost:6379`)          |
| `migrate`  | One-shot: applies `migrations/*.up.sql`                              |
| `seed`     | One-shot: loads services, zones, pincodes and technicians (re-runnable) |
| `api`      | The HTTP API on **http://localhost:8080** (healthcheck: `/readyz`)   |

Stop it and wipe the data with `docker compose down -v`.

### Configuration (env vars)

| Variable                | Default                                                            | Meaning                                    |
|-------------------------|--------------------------------------------------------------------|--------------------------------------------|
| `PORT`                  | `8080`                                                             | HTTP port                                  |
| `DATABASE_URL`          | `postgres://booking:booking@localhost:5432/booking?sslmode=disable` | Postgres connection                        |
| `REDIS_URL`             | `redis://localhost:6379/0`                                         | Redis connection                           |
| `RATE_LIMIT_REQUESTS`   | `10`                                                               | `POST /bookings` allowed per client IP per window; `0` disables the limit |
| `RATE_LIMIT_WINDOW`     | `1m`                                                               | Rate-limit window as a Go duration (`30s`, `1m`, `1h`); at least `1s` |

Compose reads `RATE_LIMIT_REQUESTS` and `RATE_LIMIT_WINDOW` from your shell or a `.env` file, e.g. `RATE_LIMIT_REQUESTS=5 RATE_LIMIT_WINDOW=30s docker compose up`.

To run the API outside Docker, start only the dependencies with `docker compose up -d postgres redis migrate seed`, then run `go run ./cmd`.

---

## Running tests

You need Go 1.26+ and Docker. The integration tests start their own Postgres and Redis containers.

```bash
go test ./...                 # everything: unit + integration + concurrency
go test -race ./...           # same, with the race detector
go test -short ./...          # unit tests only (no Docker)
go test -race -count=5 -run Concurrent -v ./internal/api   # the concurrency tests, repeated
```

| Layer | Where | What it covers |
|-------|-------|----------------|
| Pure rules | `internal/services/assignment_test.go` | Overlap, travel buffer (both directions), shift edges, 30-min alignment, 2h–7d window, cancel cutoff, skill/zone matching, ranking, slot generation |
| Services | `internal/services/*_test.go` | Booking flow against in-memory fakes: fallback to the next technician, lock timeout/outage, idempotency, cancel states, listing, availability, schedule |
| Lock | `internal/lock/redis_lock_test.go` | Real Redis: mutual exclusion, waiting, timeout, a stale holder can't release someone else's lock, Redis down |
| HTTP API | `internal/api/integration_test.go` | Real Postgres + Redis + migrations + seed through the router. **20 goroutines race for the last free technician → exactly 1×201, 19×409, 2 rows.** 10 concurrent retries with one `Idempotency-Key` → 1 row. Lifecycle, every error code, DB backstop, rate limit, request id, health |
| Seed data | `cmd/seed/main_test.go` | No pincode is in two zones, and every service has a technician in every zone |

---

## Seed data

All technicians work **08:00–20:00 IST**. The ids are fixed so the examples below work as-is.

**Services**

| id | name | duration | skill |
|----|------|----------|-------|
| `5e000000-0000-0000-0000-000000000001` | Battery replacement | 60 min | BATTERY_FIT |
| `5e000000-0000-0000-0000-000000000002` | Jump-start | 30 min | JUMPSTART |
| `5e000000-0000-0000-0000-000000000003` | Battery health check | 30 min | DIAGNOSTICS |

**Zones**: North `110001, 110002, 110003`; South `560001, 560002, 560003`.

**Technicians**

| id | name | zone | skills |
|----|------|------|--------|
| `7e000000-0000-0000-0000-000000000001` | Ravi Kumar | North | BATTERY_FIT, JUMPSTART, DIAGNOSTICS |
| `7e000000-0000-0000-0000-000000000002` | Amit Sharma | North | BATTERY_FIT |
| `7e000000-0000-0000-0000-000000000003` | Suresh Yadav | North | JUMPSTART, DIAGNOSTICS |
| `7e000000-0000-0000-0000-000000000004` | Karthik Reddy | South | BATTERY_FIT, JUMPSTART, DIAGNOSTICS |
| `7e000000-0000-0000-0000-000000000005` | Manoj Nair | South | BATTERY_FIT |
| `7e000000-0000-0000-0000-000000000006` | Arjun Rao | South | JUMPSTART, DIAGNOSTICS |

---

## API

Conventions:
- All times are **IST**. Timestamps are ISO-8601 with an offset (`2026-10-18T10:00:00+05:30`); input without an offset is read as IST. Dates are `YYYY-MM-DD`.
- Only UUIDs are exposed as ids. Internal integer keys never leave the database.
- Every response carries an `X-Request-Id` header. A well-formed incoming one is reused; otherwise one is generated. The same id appears in error bodies and on every log line for that request.

> The examples use `2026-10-18`. Replace it with a date between 2 hours and 7 days from now.

### 1. `GET /availability?serviceId=&pincode=&date=`

Returns the bookable start times. A slot is listed only if at least one qualified technician could take it.

```bash
curl "http://localhost:8080/availability?serviceId=5e000000-0000-0000-0000-000000000001&pincode=110001&date=2026-10-18"
```
```json
{"date":"2026-10-18","serviceId":"5e000000-0000-0000-0000-000000000001","pincode":110001,
 "slots":["08:00","08:30","09:00", "...", "19:00"]}
```
When nothing is free, the response is still `200`, with `"slots":[]` and `"message":"No available slots for this date"`.

### 2. `POST /bookings`

Assigns the free qualified technician who has the fewest bookings that day. Ties go to the lowest internal technician id. The `Idempotency-Key` header is optional.

```bash
curl -X POST http://localhost:8080/bookings \
  -H 'Content-Type: application/json' -H 'Idempotency-Key: 3f1c9a52-booking-1' \
  -d '{"serviceId":"5e000000-0000-0000-0000-000000000001","pincode":110001,"startTime":"2026-10-18T10:00:00+05:30","phone":"+919876543210"}'
```
`201 Created`
```json
{"id":"810858ce-ea5c-4b57-b4fb-b6e4771ea2b3","serviceId":"5e000000-0000-0000-0000-000000000001",
 "serviceName":"Battery replacement","pincode":110001,"phone":"9876543210",
 "startTime":"2026-10-18T10:00:00+05:30","endTime":"2026-10-18T11:00:00+05:30","status":"CONFIRMED",
 "technician":{"id":"7e000000-0000-0000-0000-000000000001","name":"Ravi Kumar"},
 "createdAt":"2026-10-16T14:14:02+05:30"}
```
- Retrying with the same key and body returns the **same** booking (`201`) and never creates a second one.
- Reusing the key with a different body returns `422 IDEMPOTENCY_KEY_REUSED`.
- Failed attempts, such as `409`, don't use up the key.

### 3. `GET /bookings/{id}`

```bash
curl http://localhost:8080/bookings/810858ce-ea5c-4b57-b4fb-b6e4771ea2b3
```
Returns the booking, in the same shape as above.

### 4. `POST /bookings/{id}/cancel`

```bash
curl -X POST http://localhost:8080/bookings/810858ce-ea5c-4b57-b4fb-b6e4771ea2b3/cancel
```
Returns the booking with `"status":"CANCELLED"`. The slot becomes bookable again immediately.

### 5. `GET /technicians/{id}/schedule?date=`

```bash
curl "http://localhost:8080/technicians/7e000000-0000-0000-0000-000000000001/schedule?date=2026-10-18"
```
```json
{"technician":{"id":"7e000000-0000-0000-0000-000000000001","name":"Ravi Kumar"},"date":"2026-10-18",
 "bookings":[{"id":"...","startTime":"2026-10-18T10:00:00+05:30","status":"CONFIRMED", "...": "..."}]}
```
Lists the technician's CONFIRMED and COMPLETED bookings in start order. Cancelled bookings are left out.

### Extras

**`GET /bookings?phone=&status=&from=&to=&page=&size=`**: a customer's bookings, newest first.

| param | rules |
|-------|-------|
| `phone` | **Required.** Accepts `+91` or a bare 10-digit number |
| `status` | Optional. `CONFIRMED`, `CANCELLED` or `COMPLETED` |
| `from`, `to` | Optional. Inclusive IST dates, filtering on start time |
| `page` | Default 1 |
| `size` | Default 20, max 100 |

```bash
curl "http://localhost:8080/bookings?phone=9876543210&status=CONFIRMED&page=1&size=20"
```
```json
{"items":[{"id":"...", "...": "..."}],"page":1,"size":20,"total":1}
```

**`GET /healthz`**: liveness, `{"status":"UP"}`. It doesn't check any dependency.

**`GET /readyz`**: readiness. It pings Postgres and Redis and returns `200 {"status":"UP","checks":{"postgres":"UP","redis":"UP"}}`, or `503` with the failing check marked `DOWN`.

### Errors

Every error uses this body:
```json
{"error":{"code":"NO_AVAILABILITY","message":"no qualified technician is free at that time","requestId":"…"}}
```

| HTTP | code | when |
|------|------|------|
| 400 | `VALIDATION_ERROR` | Malformed JSON or params; bad UUID, phone, pincode or timestamp; start not on a :00/:30 boundary; start outside 2h–7d from now; unknown `serviceId` in the body |
| 400 | `UNSERVICEABLE_PINCODE` | The pincode isn't in any zone |
| 404 | `NOT_FOUND` | Unknown booking or technician id in the URL, or unknown route |
| 409 | `NO_AVAILABILITY` | No qualified technician is free, including when you lose a race for the last one |
| 409 | `CANCELLATION_WINDOW_CLOSED` | Cancelling less than 1 hour before start |
| 409 | `INVALID_STATE` | Cancelling a COMPLETED booking |
| 422 | `IDEMPOTENCY_KEY_REUSED` | Same `Idempotency-Key`, different request |
| 429 | `RATE_LIMITED` | More than `RATE_LIMIT_REQUESTS` `POST /bookings` in one `RATE_LIMIT_WINDOW`. The wait is in the message, in `details.retryAfterSeconds` and in the `Retry-After` header (see below) |
| 503 | `BOOKING_BUSY` | Couldn't get the technician lock within 3s. Comes with `Retry-After: 1`; retrying with the same `Idempotency-Key` is safe |
| 503 | `DEPENDENCY_UNAVAILABLE` | Redis is unreachable (booking fails closed) or Postgres is unreachable or timing out (any endpoint). Comes with `Retry-After: 1` |
| 500 | `INTERNAL_ERROR` | Anything unexpected. Logged with the request id |

A rate-limited response tells the client how long to wait (`Retry-After: 42` is also set):
```json
{"error":{"code":"RATE_LIMITED","message":"too many booking requests, retry in 42 seconds",
 "details":{"retryAfterSeconds":42},"requestId":"…"}}
```

---

## How double-booking is prevented

1. **Redis lock per technician per day** (`lock:tech:{technicianId}:{date}`). The lock is `SET NX PX 5s` with a random token, a 25ms retry and a 3s wait. It is released by a Lua compare-and-delete, so a holder whose lock expired can't delete someone else's.
   - The lock is per technician-day, not per slot, because overlapping jobs can start at different slots (10:00 and 10:30).
   - Different days never interact, because the latest a technician can be busy is 20:30.
2. **Re-check under the lock.** The technician's bookings are re-read and the rules re-applied. If the technician has been taken, the service moves on to the next-ranked one.
3. **Database backstop.** A Postgres `EXCLUDE USING gist (technician_pk WITH =, tstzrange(start_time, blocked_until) WITH &&) WHERE status <> 'CANCELLED'` makes overlapping live bookings impossible even if a lock expires mid-write. A violation is treated as "taken, try the next technician".

The DB work inside the lock runs under a 2s timeout, well below the 5s TTL. The full reasoning, including alternatives, failure modes and scaling, is in [DESIGN.md](DESIGN.md).

---

## Assumptions

**Time and slots**
- One time zone: **IST** for everything, stored as `timestamptz`.
- Slots start on :00 or :30. A job occupies `[start, start + duration)`.

**Travel buffer and shifts**
- Every job blocks `[start, end + 30 min)`. Two jobs of the same technician conflict if those ranges overlap, in either order. So a new job placed *before* an existing one must also leave 30 minutes before it. Touching ranges are fine: a 60-min job at 08:30 fits before a 10:00 job.
- The **job** must fit inside the shift. Its trailing buffer may run past shift end, so a 60-min job at 19:00 is allowed. There's no buffer before the first job of the day.

**Booking window and assignment**
- Booking window: `now + 2h ≤ start ≤ now + 7d`, inclusive at both ends.
- "Fewest bookings that day" counts CONFIRMED and COMPLETED bookings on that IST date, but not cancelled ones. Ties go to the **lowest internal technician id**, which is deterministic and testable.
- Under a race, "fewest bookings" is best-effort: the loser falls through to the next-ranked free technician.

**Cancellation and status**
- Cancellation is allowed until exactly 1 hour before start. Cancelling an already-cancelled booking returns it unchanged (`200`). COMPLETED bookings can't be cancelled.
- No endpoint moves a booking to COMPLETED. That would come from the technician app or ops, which is out of scope.

**Identity and validation**
- There's no auth. The phone number is the customer's identity, and anyone with a booking id can cancel it. Booking ids are random UUIDs, so they can't be enumerated.
- Phone: `^(\+91)?[6-9]\d{9}$`, stored as 10 digits. Pincode: 6 digits, not starting with 0. Each pincode belongs to exactly one zone, enforced by a unique constraint.

**Idempotency**
- The key is global (not per phone), at most 255 characters. The stored fingerprint is a SHA-256 of the normalised request (service, pincode, start time, phone).

**Infrastructure behaviour**
- Rate limiting is a fixed window per client IP, per minute, shared across replicas through Redis. It fails open if Redis errors. Client IP is the socket peer, because `X-Forwarded-For` is not trusted.
- If Redis is down, `POST /bookings` fails closed (`503`). Reads and cancel keep working. If Postgres is down, every endpoint that needs it returns a retryable `503`, not a `500`.

---

## Project layout

```
cmd/main.go                 API entrypoint: config, wiring, graceful shutdown
cmd/seed/main.go            seed command (fixed UUIDs, re-runnable)
migrations/                 SQL schema (golang-migrate format)
internal/api/server.go      router + dependency wiring
internal/api/handlers/      HTTP handlers, error responder
internal/api/middleware/    request id, access log, rate limit
internal/services/          booking / availability / technician services; assignment.go holds the pure rules
internal/repository/        pgx repositories; all SQL in queries.go
internal/lock/              Redis technician lock
internal/models/            API request/response shapes
internal/domains/           DB structs + converters to API models
internal/utils/             constants, errors, IST time helpers, validation
```

---

## AI usage

**Tool:** Claude Code (Anthropic's coding agent), with two skills: *grilling* (requirements and design interview) and *to-spec* (turns that conversation into a spec).

| Used for | How |
|----------|-----|
| Requirements and design | The agent pulled every must-have and nice-to-have out of the brief, then grilled me in rounds of questions, about 40 in total, each with a recommended answer. **I made every decision**, for example: Go + Gin, a Redis lock as the primary mechanism with the Postgres exclusion constraint as backstop, integer PKs plus UUID business ids, a separate pincode table, and the project layout. |
| Specification | Generated [docs/specs/doorstep-service-booking.md](docs/specs/doorstep-service-booking.md) from that conversation. |
| Implementation | Wrote the code against the spec in small commits, each with its tests: unit tests on the pure rules, service tests with fakes, and testcontainers integration and concurrency tests. |
| Docs | Drafted this README and [DESIGN.md](DESIGN.md), which I then edited. |

**What I did:** reviewed every change before it was committed, ran the tests and the Docker stack myself, and asked for changes where I disagreed. Examples: the 429 body must not expose the rate-limit config, the limit and window should come from environment variables, and the README URLs should be explicit `http://`. I can explain and change any line of this code.
