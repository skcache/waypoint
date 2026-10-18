-- V2 M1: merchant checkout fixture tables.
--
-- These are the fixture's own tables (merchant_*), additive to the V1 schema:
-- nothing here aliases or mutates V1 intent/transaction tables, which stay
-- quarantined per docs/waypoint-v2/V1_MIGRATION.md.
--
-- Financial-safety shape enforced at the database level:
--   * money is integer minor units with a fixed USD-only currency;
--   * every provider operation carries a stable provider request id that is
--     persisted (with the order) BEFORE the outbound call is made;
--   * at most one non-terminal CAPTURE operation may exist per order, so two
--     concurrent capture attempts cannot both claim the right to call the
--     provider (the partial unique index below is the race guard);
--   * the event journal is append-only, so payment evidence cannot be
--     rewritten after the fact.

CREATE TABLE merchant_products (
    product_id  text PRIMARY KEY,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    price_minor bigint NOT NULL CHECK (price_minor > 0 AND price_minor <= 1000000000),
    currency    text NOT NULL DEFAULT 'USD' CHECK (currency = 'USD'),
    active      boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE merchant_orders (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    trace_id          uuid NOT NULL,
    product_id        text NOT NULL REFERENCES merchant_products (product_id) ON DELETE RESTRICT,
    quantity          int NOT NULL CHECK (quantity BETWEEN 1 AND 10),
    total_minor       bigint NOT NULL CHECK (total_minor > 0 AND total_minor <= 1000000000),
    currency          text NOT NULL CHECK (currency = 'USD'),
    status            text NOT NULL CHECK (status IN (
        'ORDER_CREATED',
        'BUYER_APPROVAL_PENDING',
        'APPROVED',
        'CAPTURE_IN_FLIGHT',
        'CAPTURE_OUTCOME_UNKNOWN',
        'RECONCILIATION_REQUIRED',
        'CAPTURED_VERIFIED',
        'FAILED_DEFINITE',
        'CANCELLED')),
    provider_order_id text,
    approval_url      text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX merchant_orders_trace_idx ON merchant_orders (trace_id);
CREATE UNIQUE INDEX merchant_orders_provider_id_idx
    ON merchant_orders (provider_order_id)
    WHERE provider_order_id IS NOT NULL;

CREATE TABLE merchant_operations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id            uuid NOT NULL REFERENCES merchant_orders (id) ON DELETE RESTRICT,
    operation_type      text NOT NULL CHECK (operation_type IN ('CREATE_ORDER', 'CAPTURE')),
    provider_request_id text NOT NULL,
    payload_fingerprint text NOT NULL,
    status              text NOT NULL CHECK (status IN ('IN_FLIGHT', 'SUCCEEDED', 'REJECTED', 'UNKNOWN')),
    provider_order_id   text,
    provider_capture_id text,
    attempt_count       int NOT NULL DEFAULT 1 CHECK (attempt_count >= 1),
    first_attempted_at  timestamptz NOT NULL DEFAULT now(),
    last_checked_at     timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merchant_operations_request_id_unique UNIQUE (provider_request_id)
);

-- At most one capture operation per order may be alive (IN_FLIGHT or
-- UNKNOWN). This is what makes concurrent capture attempts safe: the second
-- concurrent insert fails instead of minting a second provider request id.
CREATE UNIQUE INDEX merchant_operations_one_active_capture
    ON merchant_operations (order_id)
    WHERE operation_type = 'CAPTURE' AND status IN ('IN_FLIGHT', 'UNKNOWN');

CREATE TABLE merchant_events (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   uuid NOT NULL REFERENCES merchant_orders (id) ON DELETE RESTRICT,
    trace_id   uuid NOT NULL,
    source     text NOT NULL CHECK (source IN ('BROWSER', 'API', 'DB', 'PAYPAL', 'FAULT')),
    event_type text NOT NULL,
    detail     jsonb NOT NULL DEFAULT '{}'::jsonb,
    -- Evidence tier: R = real provider observation, F = local fault
    -- injection, S = sample. Never presented above its tier.
    tier       text NOT NULL DEFAULT 'R' CHECK (tier IN ('R', 'F', 'S')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX merchant_events_order_idx ON merchant_events (order_id, id);

CREATE OR REPLACE FUNCTION merchant_events_append_only() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'merchant_events is append-only; payment evidence cannot be rewritten';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER merchant_events_no_update_delete
    BEFORE UPDATE OR DELETE ON merchant_events
    FOR EACH ROW EXECUTE FUNCTION merchant_events_append_only();
