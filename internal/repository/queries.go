package repository

// Services

const QueryGetServiceByID = `
SELECT id, service_id, name, duration_min, required_skill
FROM services
WHERE service_id = $1`

// Zones

const QueryGetZoneByPincode = `
SELECT z.id, z.zone_id, z.name
FROM zones z
JOIN zone_pincodes zp ON zp.zone_pk = z.id
WHERE zp.pincode = $1`

// Technicians

const technicianColumns = `id, technician_id, name, zone_pk, skills, shift_start, shift_end`

const QueryGetTechnicianByID = `
SELECT ` + technicianColumns + `
FROM technicians
WHERE technician_id = $1`

const QueryListTechniciansByZoneAndSkill = `
SELECT ` + technicianColumns + `
FROM technicians
WHERE zone_pk = $1 AND $2 = ANY (skills)
ORDER BY id`

// Bookings. Every read joins services and technicians so responses carry public ids and names.

const bookingColumns = `
b.id, b.booking_id, b.customer_phone, b.service_pk, b.pincode, b.technician_pk,
b.start_time, b.end_time, b.blocked_until, b.status, b.idempotency_key, b.request_hash,
b.created_at, b.updated_at, s.service_id, s.name, t.technician_id, t.name`

const bookingJoins = `
JOIN services s ON s.id = b.service_pk
JOIN technicians t ON t.id = b.technician_pk`

const bookingSelect = `SELECT ` + bookingColumns + ` FROM bookings b ` + bookingJoins

const QueryInsertBooking = `
WITH b AS (
    INSERT INTO bookings (customer_phone, service_pk, pincode, technician_pk,
                          start_time, end_time, blocked_until, status, idempotency_key, request_hash)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
    RETURNING *
)
SELECT ` + bookingColumns + ` FROM b ` + bookingJoins

const QueryGetBookingByID = bookingSelect + `
WHERE b.booking_id = $1`

const QueryGetBookingByIdempotencyKey = bookingSelect + `
WHERE b.idempotency_key = $1`

// Live (non-cancelled) bookings of the given technicians starting in [$2, $3).
const QueryListLiveBookingsForTechnicians = bookingSelect + `
WHERE b.technician_pk = ANY ($1)
  AND b.status <> 'CANCELLED'
  AND b.start_time >= $2 AND b.start_time < $3
ORDER BY b.start_time, b.id`

// Conditional status change: only applies when the current status is $2.
const QueryTransitionBookingStatus = `
WITH b AS (
    UPDATE bookings
    SET status = $3, updated_at = now()
    WHERE id = $1 AND status = $2
    RETURNING *
)
SELECT ` + bookingColumns + ` FROM b ` + bookingJoins

// Optional filters: a NULL parameter disables its condition.
const bookingsByPhoneFilter = `
WHERE b.customer_phone = $1
  AND ($2::text IS NULL OR b.status = $2)
  AND ($3::timestamptz IS NULL OR b.start_time >= $3)
  AND ($4::timestamptz IS NULL OR b.start_time < $4)`

const QueryListBookingsByPhone = bookingSelect + bookingsByPhoneFilter + `
ORDER BY b.start_time DESC, b.id DESC
LIMIT $5 OFFSET $6`

const QueryCountBookingsByPhone = `SELECT count(*) FROM bookings b` + bookingsByPhoneFilter
