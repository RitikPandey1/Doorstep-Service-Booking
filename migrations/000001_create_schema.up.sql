-- btree_gist lets an exclusion constraint mix "=" on a bigint with "&&" on a range.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- Convention: id is the internal PK used only for relationships (FK columns are *_pk);
-- {entity}_id is the UUID business id exposed by the API.

CREATE TABLE services (
    id             BIGSERIAL PRIMARY KEY,
    service_id     UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    duration_min   INT  NOT NULL CHECK (duration_min > 0),
    required_skill TEXT NOT NULL CHECK (required_skill IN ('BATTERY_FIT', 'JUMPSTART', 'DIAGNOSTICS'))
);

CREATE TABLE zones (
    id      BIGSERIAL PRIMARY KEY,
    zone_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    name    TEXT NOT NULL UNIQUE
);

-- UNIQUE(pincode) enforces "one pincode belongs to exactly one zone".
CREATE TABLE zone_pincodes (
    id      BIGSERIAL PRIMARY KEY,
    pincode INT    NOT NULL UNIQUE CHECK (pincode BETWEEN 100000 AND 999999),
    zone_pk BIGINT NOT NULL REFERENCES zones (id)
);

CREATE TABLE technicians (
    id            BIGSERIAL PRIMARY KEY,
    technician_id UUID   NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    name          TEXT   NOT NULL,
    zone_pk       BIGINT NOT NULL REFERENCES zones (id),
    skills        TEXT[] NOT NULL CHECK (
        cardinality(skills) > 0
        AND skills <@ ARRAY ['BATTERY_FIT', 'JUMPSTART', 'DIAGNOSTICS']::TEXT[]
    ),
    shift_start   TIME   NOT NULL,
    shift_end     TIME   NOT NULL,
    CHECK (shift_start < shift_end)
);

CREATE INDEX technicians_zone_pk_idx ON technicians (zone_pk);

CREATE TABLE bookings (
    id              BIGSERIAL PRIMARY KEY,
    booking_id      UUID        NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    customer_phone  TEXT        NOT NULL CHECK (customer_phone ~ '^[6-9][0-9]{9}$'),
    service_pk      BIGINT      NOT NULL REFERENCES services (id),
    pincode         INT         NOT NULL,
    technician_pk   BIGINT      NOT NULL REFERENCES technicians (id),
    start_time      TIMESTAMPTZ NOT NULL,
    end_time        TIMESTAMPTZ NOT NULL,
    -- end_time + travel buffer; set by the app because timestamptz + interval is not IMMUTABLE.
    blocked_until   TIMESTAMPTZ NOT NULL,
    status          TEXT        NOT NULL DEFAULT 'CONFIRMED'
        CHECK (status IN ('CONFIRMED', 'CANCELLED', 'COMPLETED')),
    idempotency_key TEXT UNIQUE,
    request_hash    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (start_time < end_time AND end_time <= blocked_until),
    CHECK ((idempotency_key IS NULL) = (request_hash IS NULL)),
    -- Backstop behind the Redis lock: no two live bookings of a technician may overlap (buffer included).
    CONSTRAINT bookings_no_overlap EXCLUDE USING gist (
        technician_pk WITH =,
        tstzrange(start_time, blocked_until) WITH &&
    ) WHERE (status <> 'CANCELLED')
);

CREATE INDEX bookings_technician_start_idx ON bookings (technician_pk, start_time);
CREATE INDEX bookings_phone_start_idx ON bookings (customer_phone, start_time);
