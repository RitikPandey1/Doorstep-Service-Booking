# Spec: Doorstep Service Booking

Status: ready-for-agent
Source: Backend Developer take-home (Part A plus all nice-to-haves). DESIGN.md (Part B) and the AI-usage note are deferred.

## Problem Statement

A company sends technicians to customers' homes to service vehicle batteries. A customer needs to pick a service, give their pincode, see which time slots are actually bookable, and book one. The system must assign a free technician who has the right skill and covers that zone. It must never double-book a technician, including the travel buffer between jobs, even when many customers race for the same slot. Clients on unreliable networks retry requests, so a retry must never create a second booking. Customers also need to cancel in time, and operations needs to see each technician's day.

## Solution

A REST booking service written in Go (Gin) on PostgreSQL, with Redis for distributed locking and rate limiting.

- Customers see the free start times for a service, pincode and date, then book one with their phone number. The system picks the least-loaded qualified technician who is free.
- A Redis lock per technician per day serialises competing bookings in application code. A PostgreSQL exclusion constraint is a database-level backstop, so overlapping bookings stay impossible even if a lock expires early.
- Booking creation is idempotent via an `Idempotency-Key` header.
- Customers can cancel up to 1 hour before start, and the slot frees up at once.
- Customers can list their own bookings by phone. Technicians' daily schedules can be read.
- Every response carries a request id. Errors share one consistent body. Booking creation is rate-limited. Liveness and readiness endpoints exist.
- `docker compose up` brings up the database, Redis, migrations, seed data and the API.

## User Stories

### Discovering availability
1. As a customer, I want to see the bookable start times for a service at my pincode on a given date, so that I only choose times that can really be served.
2. As a customer, I want a slot listed only if at least one qualified technician could take it, so that I am not offered impossible times.
3. As a customer, I want slots offered on 30-minute boundaries (e.g. 08:00, 08:30), so that times are predictable.
4. As a customer, I want slots that start less than 2 hours from now hidden, so that technicians have time to reach me.
5. As a customer, I want slots more than 7 days ahead hidden, so that I only see the bookable horizon.
6. As a customer, I want slots where the job would not finish inside a technician's shift hidden, so that a 60-minute job is never offered at 19:30.
7. As a customer, I want a clear "No available slots for this date" message alongside an empty list (HTTP 200), so that I know the request worked and simply nothing is free.
8. As a customer, I want a clear error when my pincode is not served by any zone, so that I know the service does not reach me.
9. As a customer, I want a clear error when the service id or date is malformed or unknown, so that I can fix my request.
10. As a customer, I want availability to reflect cancellations immediately, so that freed slots can be booked straight away.
11. As the business, I want availability to show start times only (no technician counts or ids), so that staffing levels are not exposed.

### Booking
12. As a customer, I want to book a service by giving the service, pincode, start time and my phone number, so that a technician comes to me.
13. As a customer, I want the confirmed booking returned with the assigned technician's id and name, so that I know who is coming.
14. As a customer, I want my booking rejected if the start time is not on a 30-minute boundary, so that invalid times never enter the system.
15. As a customer, I want my booking rejected if it starts less than 2 hours or more than 7 days from now, so that the booking window is enforced.
16. As a customer, I want a clear "no availability" error when no qualified technician is free, so that I can pick another slot.
17. As a customer, I want my phone number accepted with or without a +91 prefix, so that I can type it naturally.
18. As a customer, I want an invalid phone number rejected with a clear validation error, so that I can be contacted.
19. As a customer on a flaky network, I want to retry booking with the same Idempotency-Key and get my original booking back, so that I never end up with two bookings.
20. As a customer, I want a clear error if I reuse an Idempotency-Key with different booking details, so that a mistaken key reuse does not silently return the wrong booking.
21. As a customer, I want a failed booking attempt (e.g. no availability) not to burn my Idempotency-Key, so that a later retry with the same key can still succeed.
22. As a customer, I want booking without an Idempotency-Key to still work, so that the header is optional.
23. As a customer, I want a retryable "busy" error (503 with Retry-After) when the system cannot get to my booking in time, so that I know retrying is safe.
24. As a customer, I want a clear "service temporarily unavailable" error when a dependency the booking flow needs is down, so that I am not told there is no availability when there is.

### Assignment rules
25. As the business, I want a technician assigned only to pincodes in their own zone, so that travel stays local.
26. As the business, I want a technician assigned only to services matching one of their skills, so that jobs are done by qualified people.
27. As the business, I want every job to block its full service duration, so that a 60-minute job at 10:00 occupies 10:00–11:00.
28. As a technician, I want a 30-minute travel buffer after each job before the next one starts, so that I can reach the next customer.
29. As a technician, I want the buffer enforced in both directions (a new job placed before an existing one must also leave 30 minutes before it), so that I am never late to any job.
30. As a technician, I want my first job of the day allowed right at my shift start, so that no buffer is wasted before the first job.
31. As a technician, I want a job allowed to end exactly at my shift end even though its buffer extends past it, so that the last slot of the day is usable.
32. As the business, I want the free technician with the fewest bookings that day chosen, so that work is spread evenly.
33. As the business, I want ties on booking count broken by the lowest technician (internal) id, so that assignment is deterministic and testable.
34. As the business, I want cancelled bookings excluded from the "bookings that day" count, so that load balancing reflects real work.

### Concurrency
35. As the business, I want two customers racing for the last free technician at the same slot to result in exactly one success and one clear error, so that no technician is ever double-booked.
36. As the business, I want overlapping requests for the same technician at different but overlapping slots (e.g. 10:00 and 10:30) serialised, so that overlap via different start times is also impossible.
37. As the business, I want booking requests for different technicians, or for the same technician on different days, not to block each other, so that throughput stays high.
38. As the business, I want the database to reject overlapping bookings for a technician even if the application lock fails (e.g. lock expiry during a pause or Redis failover), so that correctness never depends on the lock alone.
39. As the business, I want a racing loser to move on to the next ranked technician when its first choice was taken, so that the customer still gets a booking when someone else is free.
40. As the business, I want concurrent duplicate requests with the same Idempotency-Key to produce exactly one booking, with every response returning that booking, so that idempotency holds under races too.

### Viewing and cancelling
41. As a customer, I want to fetch a booking by its id, so that I can check its details and status.
42. As a customer, I want a clear not-found error for an unknown booking id, so that I know it does not exist.
43. As a customer, I want booking ids to be unguessable UUIDs, so that others cannot enumerate and cancel my bookings (there is no auth).
44. As a customer, I want to cancel my booking up to 1 hour before it starts, so that I can change my plans.
45. As a customer, I want cancellation at exactly 1 hour before start allowed, so that the boundary is generous and clear.
46. As a customer, I want a clear "cancellation window closed" error when I try to cancel less than 1 hour before start, so that I understand why it failed.
47. As a customer, I want cancelling an already-cancelled booking to return the booking unchanged with success, so that cancel retries are safe.
48. As a customer, I want cancelling a completed booking rejected with an invalid-state error, so that history stays accurate.
49. As a customer, I want the cancelled time to become bookable again immediately, so that someone else can take the slot.
50. As a customer, I want to list my bookings by phone number, so that I can see my upcoming and past bookings.
51. As a customer, I want to filter my bookings by status and by a from/to date range, so that I can find specific bookings.
52. As a customer, I want my booking list paginated (default 20, max 100) with a total count, newest first, so that large histories load quickly.
53. As the business, I want the bookings list to require a phone number, so that nobody can dump all bookings.
54. As an operations user, I want a technician's bookings for a date in start-time order, so that I can see their day.
55. As an operations user, I want cancelled bookings left out of a technician's schedule, so that it shows only real work.
56. As an operations user, I want a not-found error for an unknown technician id, so that typos are obvious.

### Errors, observability and operations
57. As an API client, I want every error in one consistent body (code, message, details, request id), so that I can handle errors uniformly.
58. As an API client, I want correct HTTP status codes (400, 404, 409, 422, 429, 503), so that I can react without parsing messages.
59. As an API client, I want an `X-Request-Id` I send to be echoed back (or a new one generated), so that I can correlate my calls with server logs.
60. As an operator, I want structured JSON logs with the request id on every line and one access-log line per request (method, path, status, latency), so that I can trace and debug requests.
61. As an operator, I want booking creation rate-limited per client IP (10 per minute) with 429 and Retry-After, so that abusive clients cannot exhaust capacity.
62. As an operator, I want the rate limiter to let requests through when Redis is down, so that a limiter outage does not block bookings.
63. As an operator, I want a liveness endpoint that reports the process is up without touching dependencies, so that orchestrators do not restart healthy pods during a DB blip.
64. As an operator, I want a readiness endpoint that checks PostgreSQL and Redis and returns 503 with per-dependency status when either is down, so that traffic is only routed to ready instances.
65. As an operator, I want the service to shut down gracefully and finish in-flight requests, so that deploys do not cut off bookings.
66. As a reviewer, I want `docker compose up` to start the database, Redis, migrations, seed data and the API in the right order, so that I can run everything with one command.
67. As a reviewer, I want seed data with fixed, documented ids, so that README examples work unchanged.
68. As a reviewer, I want re-running the seed to be harmless, so that restarts do not fail or duplicate data.
69. As a reviewer, I want the README to document every endpoint with a curl example, the error-code table and all assumptions, so that I can exercise the API without reading the code.
70. As a reviewer, I want `go test ./...` to run unit, integration and concurrency tests with only Docker installed, so that I can verify correctness, including live in the follow-up call.

## Implementation Decisions

### Stack and runtime
- Go with the Gin framework. PostgreSQL 16. Redis 7. Plain SQL through pgx v5 (no ORM), because the booking insert needs precise control over SQLSTATE codes. Schema in plain SQL migrations applied by golang-migrate. Logging with the standard-library structured logger (JSON).
- Config comes only from environment variables: database URL, Redis URL, port.
- Docker Compose startup order: postgres and redis healthy → one-shot migrate → one-shot seed → api. The api healthcheck uses readiness.
- All time is IST. Timestamps are stored as timestamptz. API timestamps are ISO-8601 with offset, input without an offset is treated as IST, and responses are always rendered in IST. Date params are `YYYY-MM-DD` (IST). "Now" comes from an injectable clock.

### Modules
- **Entry points:** the API binary (wiring, graceful shutdown) and a separate seed binary.
- **API layer:** server/router setup; handlers for bookings, availability, technicians and health; middleware for request id, access log and rate limit. Handlers only bind and validate input, call services, and map errors to HTTP.
- **Services:** booking service (create, get, cancel, list-by-phone, idempotency), availability service, technician service (schedule). A separate pure **assignment** module holds all rules as side-effect-free functions: slot generation, overlap with buffer, shift fit, booking window, candidate ranking. Services depend on repository and lock **interfaces** so they can be tested with fakes.
- **Repository:** one repository per aggregate (booking, technician, service, zone). All SQL lives in one constants file in the repository package.
- **Lock:** a `Locker` interface with a Redis implementation (acquire with wait and timeout, release by token).
- **Models:** API request/response DTOs only. They import no other internal package.
- **Domains:** DB-facing structs plus **all** converters between domains and models (e.g. booking → response, request → new booking). They live here to avoid an import cycle.
- **Utils:** shared constants, the application error type (code + HTTP status), IST/time helpers, phone normaliser.

### Schema
- Every table has an internal auto-increment integer primary key, used only for relationships and never exposed. Every exposed entity also has a UUID business id named `{entity}_id` (e.g. `service_id`, `technician_id`, `booking_id`). The API exposes only the UUID, as `"id"`. Foreign-key columns are named `{entity}_pk` (e.g. `technician_pk`).
- `services`: name, duration in minutes, required skill.
- `zones`: name.
- `zone_pincodes`: pincode (integer, unique, which enforces one zone per pincode) and its zone. No UUID because it is never exposed.
- `technicians`: name, zone, skills (text array), shift start and shift end (time of day).
- `bookings`: customer phone (normalised 10 digits), service, pincode (integer), technician, start time, end time, blocked-until (end + 30 min, computed in the application because timestamptz + interval is not immutable), status, nullable unique idempotency key, request hash, created/updated timestamps.
- Status and skill are text with CHECK constraints, not Postgres enums. Status values are CONFIRMED, CANCELLED, COMPLETED. Skill values are BATTERY_FIT, JUMPSTART, DIAGNOSTICS.
- Indexes on bookings: (technician, start time) and (customer phone, start time).
- **Backstop:** a btree_gist exclusion constraint on bookings that forbids two non-cancelled bookings for the same technician whose `[start_time, blocked_until)` ranges overlap.

### Seed
- The three services from the brief: Battery replacement (60 min, BATTERY_FIT), Jump-start (30, JUMPSTART), Battery health check (30, DIAGNOSTICS).
- Two zones: North (110001, 110002, 110003) and South (560001, 560002, 560003).
- Six technicians, three per zone, all on 08:00–20:00. In each zone: one with all skills, one with BATTERY_FIT only, one with JUMPSTART + DIAGNOSTICS.
- Fixed hard-coded UUIDs. Inserts are no-ops on conflict, so the seed can be re-run safely.

### Domain rules
- Slots start on :00 or :30. A job occupies `[start, start + duration)` and blocks `[start, end + 30 min)`.
- Two jobs of the same technician conflict if their blocked ranges overlap, in either order.
- The job, not its buffer, must lie fully within the technician's shift. There is no buffer before the first job.
- Booking window: `now + 2h ≤ start ≤ now + 7d`, inclusive. Availability for a date outside the window returns an empty list with a message.
- A technician qualifies if their zone contains the pincode and their skills include the service's required skill.
- Ranking: fewest CONFIRMED + COMPLETED bookings on that IST date, then lowest internal technician id.
- Cancellation is allowed if `now ≤ start − 1h`. CANCELLED → CANCELLED returns 200 unchanged. COMPLETED → cancel returns 409 INVALID_STATE. No ownership check (no auth).
- No endpoint or job moves a booking to COMPLETED. The status exists for completeness.
- Phone must match `^(\+91)?[6-9]\d{9}$` and is stored as 10 digits. Pincode must match `^[1-9]\d{5}$`.

### Booking creation flow
1. Validate input (fields, phone, pincode, 30-min boundary, booking window, service exists).
2. If an Idempotency-Key is present and already stored: same request hash → return the original booking with 201. Different hash → 422 IDEMPOTENCY_KEY_REUSED.
3. Resolve the zone for the pincode (400 UNSERVICEABLE_PINCODE if none). Load the qualified technicians and their bookings for the day. Compute the free candidates and rank them.
4. For each candidate in order:
   - Acquire the Redis lock keyed by technician UUID and IST date. Wait up to 3 s, polling about every 25 ms. The lock TTL is 5 s and the token is random.
   - While holding the lock, with a 2 s DB timeout, re-read that technician's bookings, re-check the overlap, then insert.
   - Release the lock with an atomic compare-and-delete on the token.
   - If the technician turned out to be busy, or the insert hit the exclusion constraint (SQLSTATE 23P01), try the next candidate.
   - If the insert hit the idempotency-key unique constraint (23505), fetch and return the winner's booking.
5. If no candidate succeeds: 409 NO_AVAILABILITY. If the lock wait times out: 503 BOOKING_BUSY with `Retry-After: 1`. If Redis is unreachable: 503 DEPENDENCY_UNAVAILABLE (fail closed). GET and cancel endpoints do not use locks.
6. Why lock per technician-day rather than per slot: a per-slot key would let overlapping jobs at different start times (10:00 and 10:30) both pass. Different days never interact because the latest blocked time is 20:30.
7. Why the Redis lock is the primary mechanism: locking stays independent of the database choice. The exclusion constraint is a Postgres-specific safety net for lock expiry and Redis failover.

### API contracts
- `GET /availability?serviceId=&pincode=&date=` returns `{date, serviceId, pincode, slots: ["HH:MM", …]}`, plus `message` only when `slots` is empty (still HTTP 200).
- `POST /bookings` takes body `{serviceId, pincode, startTime, phone}` and an optional `Idempotency-Key` header. It returns 201 with the booking.
- Booking representation: `{id, serviceId, serviceName, pincode, phone, startTime, endTime, status, technician: {id, name}, createdAt}`.
- `GET /bookings/{id}` returns the booking.
- `POST /bookings/{id}/cancel` returns the booking.
- `GET /bookings?phone=&status=&from=&to=&page=&size=`: phone is required, default size 20, max 100, sorted by start time descending. It returns `{items, page, size, total}`.
- `GET /technicians/{id}/schedule?date=` returns `{technician: {id, name}, date, bookings: [...]}`, with non-cancelled bookings in start-time order.
- `GET /healthz` (liveness, always 200 while running) and `GET /readyz` (pings Postgres and Redis; 200 or 503 with per-dependency status).
- **Errors** all use the body `{"error": {"code", "message", "details", "requestId"}}`:
  - 400: VALIDATION_ERROR, UNSERVICEABLE_PINCODE. An unknown id inside a body or query param is a 400.
  - 404: NOT_FOUND, only for ids in the URL path.
  - 409: NO_AVAILABILITY, CANCELLATION_WINDOW_CLOSED, INVALID_STATE.
  - 422: IDEMPOTENCY_KEY_REUSED.
  - 429: RATE_LIMITED, with Retry-After.
  - 503: BOOKING_BUSY, with Retry-After. DEPENDENCY_UNAVAILABLE.

### Cross-cutting
- Request id: reuse the incoming `X-Request-Id` or generate a UUID. Store it in the request context and logger. Echo it in the response header and in error bodies.
- Rate limit: a Redis fixed window keyed by client IP and minute, 10 `POST /bookings` per minute, applied only to that route. Fail open if Redis errors.
- Graceful shutdown on SIGINT/SIGTERM.

## Testing Decisions

- **What makes a good test:** assert only on external behaviour: return values of pure rules, service outcomes and errors, HTTP status codes, response bodies and resulting database rows. Never assert on internal call order, SQL text or lock internals. Tests control "now" through the injectable clock, never wall time.
- **Seam 1, pure assignment rules (unit, no DB, table-driven):** slot generation per duration; overlap including the buffer in both directions; touching ranges (end + 30 = next start) are allowed; shift edges (first slot at shift start, last job ending exactly at shift end allowed, a job crossing shift end rejected); 30-minute boundary; booking window edges (exactly 2 h and exactly 7 d allowed, just outside rejected); skill and zone filtering; ranking by day count then id; cancellation-window edge (exactly 1 h allowed).
- **Seam 2, services with fake repositories and a fake locker:** the try-next-candidate loop; lock-timeout → BOOKING_BUSY; locker unavailable → DEPENDENCY_UNAVAILABLE; idempotency replay and mismatch; cancel state transitions.
- **Seam 3, HTTP API against real infrastructure (integration):** testcontainers-go starts PostgreSQL and Redis, applies the migrations, runs the seed and serves the real router via an in-process test server. It covers:
  - Happy paths for every endpoint.
  - Every error code.
  - A **concurrency test:** 20 goroutines book the same slot where exactly one technician qualifies. Expect exactly one 201, nineteen 409 NO_AVAILABILITY, and exactly one row in the database.
  - An **idempotency race test:** 10 goroutines send the same Idempotency-Key. Expect one row and identical responses.
  - A test that the exclusion-constraint backstop rejects a directly inserted overlapping booking.
  - A test that a cancelled slot becomes bookable again.
- A seed test asserts that no pincode maps to two zones and that every service has at least one qualified technician per zone.
- Prior art: none. The repository is empty, so these tests set the conventions.

## Out of Scope

- Authentication and authorisation. The phone number is the customer's identity, and cancel needs only the booking id.
- Admin APIs for services, zones or technicians (seed only).
- Any transition to COMPLETED (no endpoint, no scheduler).
- Technician absence and leave, reassignment, and notifications (SMS/WhatsApp/push). These are for DESIGN.md discussion only.
- Multi-timezone support (IST only).
- Caching of availability, read replicas, partitioning, and a Redis fast path for idempotency.
- OpenAPI spec (README curl examples instead).
- DESIGN.md (Part B) and the README AI-usage section: deferred, to be discussed separately.

## Further Notes

- Known limits:
  - "Fewest bookings that day" is best-effort under races, because a racing loser falls to the next technician.
  - Two non-overlapping requests for the same technician-day briefly serialise on the lock.
  - Pincode lookup has no special index at two zones.
- Commit history: about 13 focused commits (scaffold, migrations, seed, rules + unit tests, repositories, lock, booking service, availability + schedule, HTTP layer, integration + concurrency tests, nice-to-haves, README, DESIGN.md). Commit messages must not carry any Claude attribution or co-author trailer.
- The follow-up call will run the concurrency test live and extend the code, so every part must stay small and explainable.
