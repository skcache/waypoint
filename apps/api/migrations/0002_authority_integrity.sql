-- Phase 02 remediation (review F01–F03): database-enforced authority integrity.
--
-- Adds, additively (0001 is applied and checksummed; this migration never
-- edits it):
--   F01  intent_contract_roots        — one authoritative owner per logical
--        contract; every version must match it (declarative composite FK), so
--        versions can never change ownership. transactions gains a composite
--        FK (contract_id, contract_version, principal_id), so a transaction
--        can only bind ITS OWN principal's contract version.
--   F03  the confirmed-contract trigger becomes confirmed + NOT EXPIRED using
--        database time. A partial unique index cannot back a foreign key in
--        PostgreSQL, so a trigger remains the mechanism for "confirmed only".
--        PHASE 03 REQUIREMENT: executions must re-validate contract validity
--        at the moment of execution — a transaction created while a contract
--        was valid may be executed after its expiry. This migration only
--        guards transaction creation.
--   F02  authorized transaction details become immutable (see below).
--
-- APPLICATION CONTRACT: create the intent_contract_roots row (for the
-- principal that will own the contract) BEFORE inserting version 1.

-- ---------------------------------------------------------------------------
-- F01: logical contract ownership anchor.
-- ---------------------------------------------------------------------------
CREATE TABLE intent_contract_roots (
    contract_id  uuid PRIMARY KEY,
    principal_id uuid NOT NULL REFERENCES demo_sessions (id) ON DELETE RESTRICT,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT intent_contract_roots_owner_unique UNIQUE (contract_id, principal_id)
);

COMMENT ON TABLE intent_contract_roots IS
    'One row per logical intent contract; owns the authoritative principal. All contract versions must share it and transactions bind through it.';

-- Upgrade path: adopt existing contracts (their version 1 defines ownership;
-- version rows are already contiguous and, if any historical row had a
-- different principal, the FK below fails loudly rather than guessing).
INSERT INTO intent_contract_roots (contract_id, principal_id)
SELECT contract_id, principal_id
  FROM intent_contracts
 WHERE version = 1;

ALTER TABLE intent_contracts
    ADD CONSTRAINT intent_contracts_root_fk
    FOREIGN KEY (contract_id, principal_id)
    REFERENCES intent_contract_roots (contract_id, principal_id)
    ON DELETE RESTRICT;

-- FK target for the principal-bound transaction reference.
ALTER TABLE intent_contracts
    ADD CONSTRAINT intent_contracts_version_principal_unique
    UNIQUE (contract_id, version, principal_id);

-- ---------------------------------------------------------------------------
-- F01: transactions bind only their own principal's authority.
-- Replaces the principal-blind two-column FK.
-- ---------------------------------------------------------------------------
ALTER TABLE transactions
    DROP CONSTRAINT transactions_contract_fk;

ALTER TABLE transactions
    ADD CONSTRAINT transactions_contract_principal_fk
    FOREIGN KEY (contract_id, contract_version, principal_id)
    REFERENCES intent_contracts (contract_id, version, principal_id)
    ON DELETE RESTRICT;

-- ---------------------------------------------------------------------------
-- F01 + F03: creation requires existing, own-principal, confirmed, unexpired
-- authority (database time). The principal check also has a declarative
-- backstop via the composite FK above.
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION transactions_require_confirmed_contract() RETURNS trigger AS $$
DECLARE
    contract_principal uuid;
    confirmed          timestamptz;
    expires            timestamptz;
BEGIN
    SELECT principal_id, confirmed_at, expires_at
      INTO contract_principal, confirmed, expires
      FROM intent_contracts
     WHERE contract_id = NEW.contract_id AND version = NEW.contract_version;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'transaction references an unknown intent contract version (contract %, version %)',
            NEW.contract_id, NEW.contract_version;
    END IF;
    IF contract_principal <> NEW.principal_id THEN
        RAISE EXCEPTION 'intent contract version belongs to a different principal (contract %, version %)',
            NEW.contract_id, NEW.contract_version;
    END IF;
    IF confirmed IS NULL THEN
        RAISE EXCEPTION 'transaction requires a confirmed intent contract version (contract %, version %)',
            NEW.contract_id, NEW.contract_version;
    END IF;
    IF expires <= now() THEN
        RAISE EXCEPTION 'transaction authority is expired (contract %, version %, expired at %)',
            NEW.contract_id, NEW.contract_version, expires;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION transactions_require_confirmed_contract() IS
    'Creation-time guard: contract version must exist, belong to the same principal, be confirmed, and be unexpired (database time). Execution-time re-validation is a Phase 03 requirement.';

-- ---------------------------------------------------------------------------
-- F02: authorized transaction details are immutable. Execution state
-- (state, updated_at) stays mutable; everything that defines WHAT was
-- authorized is frozen at insert. created_at is included: history must not
-- be rewritable either.
-- ---------------------------------------------------------------------------
CREATE FUNCTION transactions_authorized_details_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.principal_id          IS DISTINCT FROM OLD.principal_id
       OR NEW.contract_id        IS DISTINCT FROM OLD.contract_id
       OR NEW.contract_version   IS DISTINCT FROM OLD.contract_version
       OR NEW.client_operation_key IS DISTINCT FROM OLD.client_operation_key
       OR NEW.catalog_item_id    IS DISTINCT FROM OLD.catalog_item_id
       OR NEW.catalog_item_version IS DISTINCT FROM OLD.catalog_item_version
       OR NEW.quantity           IS DISTINCT FROM OLD.quantity
       OR NEW.amount_minor       IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency           IS DISTINCT FROM OLD.currency
       OR NEW.action_fingerprint IS DISTINCT FROM OLD.action_fingerprint
       OR NEW.created_at         IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'authorized transaction details are immutable; only execution state may change (transaction %)',
            OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER transactions_authorized_details_immutable
    BEFORE UPDATE ON transactions
    FOR EACH ROW EXECUTE FUNCTION transactions_authorized_details_immutable();
