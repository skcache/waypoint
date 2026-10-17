-- Waypoint V1 durable schema (PRD §7.6, adapted for the locked V1 architecture).
--
-- Money: integer minor units only (bigint *_minor columns). No floating point.
-- Currency: USD-only for this release; enforced with CHECK constraints.
--
-- Mutability classes:
--   IMMUTABLE (append-only rows, enforced by triggers below):
--     catalog_items            (a price change means a new version row)
--     semantic_verifications   (classification records)
--     transaction_events       (causal log)
--     intent_contracts         (draft rows may be confirmed exactly once;
--                               confirmed rows are immutable; edits create a
--                               new version row)
--   MUTABLE (execution state):
--     demo_sessions, transactions, payment_operations, provider_webhook_events
--
-- Limits are per the product spec (values in minor units): the 1_000_000_000
-- cap (10,000,000.00 USD) is a hard sanity bound; per-contract ceilings are
-- enforced by the application against the confirmed intent contract.

-- --------------------------------------------------------------------------
-- demo_sessions: bounded demo identity. Only token hashes are stored.
-- --------------------------------------------------------------------------
CREATE TABLE demo_sessions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash  bytea NOT NULL UNIQUE,          -- SHA-256 of the session token
    csrf_hash   bytea NOT NULL UNIQUE,          -- SHA-256 of the CSRF token
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    CONSTRAINT demo_sessions_expiry_after_creation CHECK (expires_at > created_at),
    CONSTRAINT demo_sessions_token_hash_len   CHECK (octet_length(token_hash) = 32),
    CONSTRAINT demo_sessions_csrf_hash_len    CHECK (octet_length(csrf_hash) = 32)
);

-- --------------------------------------------------------------------------
-- intent_contracts: human intent drafts and confirmed authority.
-- One row per contract version. Confirmation is a one-way transition on the
-- same row; any edit must create a new version row (version = previous + 1).
-- --------------------------------------------------------------------------
CREATE TABLE intent_contracts (
    contract_id             uuid NOT NULL,
    version                 integer NOT NULL,
    principal_id            uuid NOT NULL REFERENCES demo_sessions (id) ON DELETE RESTRICT,
    original_request        text NOT NULL,
    currency                text,
    max_total_minor         bigint,
    max_quantity            integer,
    category                text,
    qualitative_preferences text[] NOT NULL DEFAULT '{}',
    canonical_hash          bytea,
    confirmed_at            timestamptz,
    expires_at              timestamptz,
    created_at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (contract_id, version),
    CONSTRAINT intent_contracts_version_positive  CHECK (version >= 1),
    CONSTRAINT intent_contracts_request_nonempty  CHECK (length(btrim(original_request)) > 0),
    CONSTRAINT intent_contracts_request_bounded   CHECK (length(original_request) <= 4000),
    CONSTRAINT intent_contracts_usd_only          CHECK (currency IS NULL OR currency = 'USD'),
    CONSTRAINT intent_contracts_max_total_bounds  CHECK (max_total_minor IS NULL OR (max_total_minor > 0 AND max_total_minor <= 1000000000)),
    CONSTRAINT intent_contracts_max_quantity_bounds CHECK (max_quantity IS NULL OR (max_quantity >= 1 AND max_quantity <= 1000)),
    CONSTRAINT intent_contracts_hash_len          CHECK (canonical_hash IS NULL OR octet_length(canonical_hash) = 32),
    -- A confirmed version must be complete; a draft may have unresolved fields.
    CONSTRAINT intent_contracts_confirmed_complete CHECK (
        confirmed_at IS NULL
        OR (currency IS NOT NULL AND max_total_minor IS NOT NULL AND max_quantity IS NOT NULL
            AND canonical_hash IS NOT NULL AND expires_at IS NOT NULL)
    ),
    CONSTRAINT intent_contracts_expiry_after_confirmation CHECK (expires_at IS NULL OR expires_at > confirmed_at)
);

CREATE FUNCTION intent_contracts_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.confirmed_at IS NOT NULL THEN
            RAISE EXCEPTION 'confirmed intent contract versions are immutable (contract %, version %)',
                OLD.contract_id, OLD.version;
        END IF;
        RETURN OLD;
    END IF;

    -- UPDATE
    IF OLD.confirmed_at IS NOT NULL THEN
        RAISE EXCEPTION 'confirmed intent contract versions are immutable (contract %, version %)',
            OLD.contract_id, OLD.version;
    END IF;
    IF NEW.contract_id IS DISTINCT FROM OLD.contract_id
       OR NEW.version IS DISTINCT FROM OLD.version
       OR NEW.principal_id IS DISTINCT FROM OLD.principal_id
       OR NEW.original_request IS DISTINCT FROM OLD.original_request
       OR NEW.currency IS DISTINCT FROM OLD.currency
       OR NEW.max_total_minor IS DISTINCT FROM OLD.max_total_minor
       OR NEW.max_quantity IS DISTINCT FROM OLD.max_quantity
       OR NEW.category IS DISTINCT FROM OLD.category
       OR NEW.qualitative_preferences IS DISTINCT FROM OLD.qualitative_preferences
    THEN
        RAISE EXCEPTION 'intent contract edits must create a new version row (contract %, version %)',
            OLD.contract_id, OLD.version;
    END IF;
    IF NEW.confirmed_at IS NULL THEN
        RAISE EXCEPTION 'the only permitted update to a draft intent contract version is its confirmation (contract %, version %)',
            OLD.contract_id, OLD.version;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER intent_contracts_guard
    BEFORE UPDATE OR DELETE ON intent_contracts
    FOR EACH ROW EXECUTE FUNCTION intent_contracts_guard();

-- Versions must be contiguous: v1 first, then v2, ... (no gaps, no rewrites).
CREATE FUNCTION intent_contracts_version_sequence() RETURNS trigger AS $$
DECLARE
    max_version integer;
BEGIN
    SELECT max(version) INTO max_version
      FROM intent_contracts WHERE contract_id = NEW.contract_id;

    IF max_version IS NULL THEN
        IF NEW.version <> 1 THEN
            RAISE EXCEPTION 'first version of intent contract % must be 1', NEW.contract_id;
        END IF;
    ELSIF NEW.version <> max_version + 1 THEN
        RAISE EXCEPTION 'intent contract % versions must be contiguous: next version is %, got %',
            NEW.contract_id, max_version + 1, NEW.version;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER intent_contracts_version_sequence
    BEFORE INSERT ON intent_contracts
    FOR EACH ROW EXECUTE FUNCTION intent_contracts_version_sequence();

-- --------------------------------------------------------------------------
-- catalog_items: controlled synthetic fixtures. Immutable per version.
-- --------------------------------------------------------------------------
CREATE TABLE catalog_items (
    item_id      text NOT NULL,
    version      integer NOT NULL,
    name         text NOT NULL,
    description  text NOT NULL,
    price_minor  bigint NOT NULL,
    currency     text NOT NULL DEFAULT 'USD',
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (item_id, version),
    CONSTRAINT catalog_items_version_positive     CHECK (version >= 1),
    CONSTRAINT catalog_items_name_nonempty        CHECK (length(btrim(name)) > 0),
    CONSTRAINT catalog_items_description_nonempty CHECK (length(btrim(description)) > 0),
    CONSTRAINT catalog_items_price_bounds         CHECK (price_minor > 0 AND price_minor <= 1000000000),
    CONSTRAINT catalog_items_usd_only             CHECK (currency = 'USD')
);

CREATE FUNCTION catalog_items_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'catalog item versions are immutable; insert a new version row instead';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER catalog_items_immutable
    BEFORE UPDATE OR DELETE ON catalog_items
    FOR EACH ROW EXECUTE FUNCTION catalog_items_immutable();

-- --------------------------------------------------------------------------
-- transactions: the logical unit of purchase authority + execution state.
-- MUTABLE (state advances). Authority fields (contract/item bindings, amount,
-- fingerprint) are set at insert from server-owned data.
-- --------------------------------------------------------------------------
CREATE TABLE transactions (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    principal_id         uuid NOT NULL REFERENCES demo_sessions (id) ON DELETE RESTRICT,
    contract_id          uuid NOT NULL,
    contract_version     integer NOT NULL,
    client_operation_key text NOT NULL,
    catalog_item_id      text NOT NULL,
    catalog_item_version integer NOT NULL,
    quantity             integer NOT NULL,
    amount_minor         bigint NOT NULL,
    currency             text NOT NULL,
    action_fingerprint   bytea NOT NULL,
    state                text NOT NULL DEFAULT 'PROPOSED',
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transactions_contract_fk FOREIGN KEY (contract_id, contract_version)
        REFERENCES intent_contracts (contract_id, version) ON DELETE RESTRICT,
    CONSTRAINT transactions_catalog_fk FOREIGN KEY (catalog_item_id, catalog_item_version)
        REFERENCES catalog_items (item_id, version) ON DELETE RESTRICT,
    CONSTRAINT transactions_operation_key_unique UNIQUE (principal_id, client_operation_key),
    CONSTRAINT transactions_operation_key_nonempty CHECK (length(btrim(client_operation_key)) > 0),
    CONSTRAINT transactions_quantity_bounds  CHECK (quantity >= 1 AND quantity <= 1000),
    CONSTRAINT transactions_amount_bounds    CHECK (amount_minor > 0 AND amount_minor <= 1000000000),
    CONSTRAINT transactions_usd_only         CHECK (currency = 'USD'),
    CONSTRAINT transactions_fingerprint_len  CHECK (octet_length(action_fingerprint) = 32),
    CONSTRAINT transactions_state_valid CHECK (state IN (
        'PROPOSED','VERIFYING','DENIED','REVIEW_REQUIRED','AUTHORIZED','ORDER_CREATING',
        'AWAITING_BUYER_APPROVAL','CANCELLED','CAPTURE_PENDING','COMPLETED',
        'RECONCILIATION_REQUIRED','FAILED'))
);

CREATE INDEX transactions_contract_idx  ON transactions (contract_id, contract_version);
CREATE INDEX transactions_principal_idx ON transactions (principal_id);

-- A transaction may only bind to a *confirmed* contract version.
CREATE FUNCTION transactions_require_confirmed_contract() RETURNS trigger AS $$
DECLARE
    confirmed timestamptz;
BEGIN
    SELECT confirmed_at INTO confirmed
      FROM intent_contracts
     WHERE contract_id = NEW.contract_id AND version = NEW.contract_version;

    IF confirmed IS NULL THEN
        RAISE EXCEPTION 'transaction requires a confirmed intent contract version (contract %, version %)',
            NEW.contract_id, NEW.contract_version;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER transactions_require_confirmed_contract
    BEFORE INSERT ON transactions
    FOR EACH ROW EXECUTE FUNCTION transactions_require_confirmed_contract();

CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER transactions_touch
    BEFORE UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- --------------------------------------------------------------------------
-- payment_operations: one row per provider operation (create / capture).
-- MUTABLE (attempts, reconciliation). Provider request IDs are persisted
-- before any outbound call and are unique system-wide.
-- --------------------------------------------------------------------------
CREATE TABLE payment_operations (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id      uuid NOT NULL REFERENCES transactions (id) ON DELETE RESTRICT,
    operation_type      text NOT NULL,
    paypal_request_id   text NOT NULL,
    payload_fingerprint bytea NOT NULL,
    provider_resource_id text,
    status              text NOT NULL DEFAULT 'PENDING',
    attempt_count       integer NOT NULL DEFAULT 0,
    first_attempted_at  timestamptz,
    last_checked_at     timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT payment_operations_type_valid CHECK (operation_type IN ('CREATE_ORDER','CAPTURE')),
    CONSTRAINT payment_operations_identity_unique UNIQUE (transaction_id, operation_type),
    CONSTRAINT payment_operations_request_id_unique UNIQUE (paypal_request_id),
    CONSTRAINT payment_operations_request_id_nonempty CHECK (length(btrim(paypal_request_id)) > 0),
    CONSTRAINT payment_operations_status_valid CHECK (status IN ('PENDING','SUCCEEDED','FAILED','UNKNOWN')),
    CONSTRAINT payment_operations_attempts_nonnegative CHECK (attempt_count >= 0),
    CONSTRAINT payment_operations_fingerprint_len CHECK (octet_length(payload_fingerprint) = 32)
);

CREATE TRIGGER payment_operations_touch
    BEFORE UPDATE ON payment_operations
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- --------------------------------------------------------------------------
-- semantic_verifications: model classification records. Append-only.
-- --------------------------------------------------------------------------
CREATE TABLE semantic_verifications (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id  uuid NOT NULL REFERENCES transactions (id) ON DELETE RESTRICT,
    classification  text NOT NULL,
    model           text NOT NULL,
    evidence        jsonb NOT NULL DEFAULT '[]'::jsonb,
    input_tokens    integer,
    output_tokens   integer,
    cost_micro_usd  bigint,
    latency_ms      integer,
    error           text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT semantic_verifications_classification_valid
        CHECK (classification IN ('SUPPORTED','CONTRADICTED','UNKNOWN')),
    CONSTRAINT semantic_verifications_model_nonempty CHECK (length(btrim(model)) > 0),
    CONSTRAINT semantic_verifications_tokens_nonnegative
        CHECK ((input_tokens IS NULL OR input_tokens >= 0) AND (output_tokens IS NULL OR output_tokens >= 0)),
    CONSTRAINT semantic_verifications_cost_nonnegative CHECK (cost_micro_usd IS NULL OR cost_micro_usd >= 0),
    CONSTRAINT semantic_verifications_latency_nonnegative CHECK (latency_ms IS NULL OR latency_ms >= 0)
);

CREATE INDEX semantic_verifications_transaction_idx ON semantic_verifications (transaction_id);

-- --------------------------------------------------------------------------
-- transaction_events: append-only causal log. seq is monotonic per
-- transaction; (transaction_id, seq) is unique.
-- --------------------------------------------------------------------------
CREATE TABLE transaction_events (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    transaction_id  uuid NOT NULL REFERENCES transactions (id) ON DELETE RESTRICT,
    seq             integer NOT NULL,
    event_type      text NOT NULL,
    source          text NOT NULL,
    payload         jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT transaction_events_order_unique UNIQUE (transaction_id, seq),
    CONSTRAINT transaction_events_seq_positive CHECK (seq >= 1),
    CONSTRAINT transaction_events_type_nonempty CHECK (length(btrim(event_type)) > 0),
    CONSTRAINT transaction_events_source_valid CHECK (source IN ('MODEL','POLICY','RUNTIME','PAYPAL'))
);

-- Shared append-only guard for semantic_verifications and transaction_events.
CREATE FUNCTION append_only_guard() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION '% is append-only; % is not permitted', TG_TABLE_NAME, TG_OP;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER semantic_verifications_append_only
    BEFORE UPDATE OR DELETE ON semantic_verifications
    FOR EACH ROW EXECUTE FUNCTION append_only_guard();

CREATE TRIGGER transaction_events_append_only
    BEFORE UPDATE OR DELETE ON transaction_events
    FOR EACH ROW EXECUTE FUNCTION append_only_guard();

-- --------------------------------------------------------------------------
-- provider_webhook_events: deduplication + processing state for PayPal
-- notifications. MUTABLE (processing state). Unverified events may be
-- recorded but never change payment state (application invariant, Phase 15).
-- --------------------------------------------------------------------------
CREATE TABLE provider_webhook_events (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_event_id   text NOT NULL UNIQUE,
    dedup_key           text NOT NULL UNIQUE,
    verification_result text NOT NULL,
    processing_state    text NOT NULL DEFAULT 'RECEIVED',
    received_at         timestamptz NOT NULL DEFAULT now(),
    processed_at        timestamptz,
    CONSTRAINT provider_webhook_events_event_id_nonempty CHECK (length(btrim(provider_event_id)) > 0),
    CONSTRAINT provider_webhook_events_dedup_key_nonempty CHECK (length(btrim(dedup_key)) > 0),
    CONSTRAINT provider_webhook_events_verification_valid
        CHECK (verification_result IN ('VERIFIED','REJECTED','UNVERIFIED')),
    CONSTRAINT provider_webhook_events_state_valid
        CHECK (processing_state IN ('RECEIVED','APPLIED','IGNORED','FAILED'))
);
