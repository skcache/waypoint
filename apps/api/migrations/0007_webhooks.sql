-- M4: durable webhook receipt identity and fulfillment effects.
--
-- Webhook deliveries are at-least-once: the same event can arrive twice, an
-- older event can arrive after a newer one, and handlers can race. The fix is
-- durable identity plus transactional uniqueness, not in-memory dedupe:
--
--   * merchant_webhook_events.event_id is UNIQUE, so a redelivery updates the
--     same receipt (deliveries++) instead of creating a second one;
--   * merchant_fulfillment_effects has a stable (order_id, effect_key) key, so
--     a legitimate payment can produce at most one fulfillment effect even if
--     several distinct events describe the same capture;
--   * authenticated records only: the fixture verifier is tier F and the
--     column keeps that provenance explicit.
CREATE TABLE merchant_webhook_events (
    id                  bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id            text NOT NULL,
    event_type          text NOT NULL,
    order_id            uuid REFERENCES merchant_orders (id) ON DELETE RESTRICT,
    provider_order_id   text,
    provider_capture_id text,
    payload_digest      text NOT NULL,
    authenticated       boolean NOT NULL,
    processed           boolean NOT NULL DEFAULT false,
    deliveries          int NOT NULL DEFAULT 1 CHECK (deliveries >= 1),
    tier                text NOT NULL CHECK (tier IN ('R', 'F', 'S')),
    first_received_at   timestamptz NOT NULL DEFAULT now(),
    last_received_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merchant_webhook_events_event_id_unique UNIQUE (event_id)
);

CREATE INDEX merchant_webhook_events_order_idx ON merchant_webhook_events (order_id);

CREATE TABLE merchant_fulfillment_effects (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id   uuid NOT NULL REFERENCES merchant_orders (id) ON DELETE RESTRICT,
    effect_key text NOT NULL,
    event_id   text NOT NULL,
    tier       text NOT NULL CHECK (tier IN ('R', 'F', 'S')),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT merchant_fulfillment_once UNIQUE (order_id, effect_key)
);

CREATE INDEX merchant_fulfillment_effects_order_idx ON merchant_fulfillment_effects (order_id);
