-- 0003_identity_integrity.sql
-- Final Phase 02 integrity patch (review of 2fa0246):
--   * transactions.id joins the protected authorized-detail set — the primary
--     key is part of the authorized identity and must never be rewritten.
--   * payment_operations identity becomes immutable; provider_resource_id may
--     transition NULL -> value exactly once (the provider result becoming
--     known) and must never silently change afterwards.
-- Additive only: no historic migration is altered and no existing row changes.

-- ---------------------------------------------------------------------------
-- Transaction ID: protected alongside every other authorized detail.
-- (The trigger from 0002 already points at this function name; replacing the
-- body is sufficient — the trigger definition stays untouched.)
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION transactions_authorized_details_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.principal_id          IS DISTINCT FROM OLD.principal_id
       OR NEW.contract_id           IS DISTINCT FROM OLD.contract_id
       OR NEW.contract_version      IS DISTINCT FROM OLD.contract_version
       OR NEW.client_operation_key  IS DISTINCT FROM OLD.client_operation_key
       OR NEW.catalog_item_id       IS DISTINCT FROM OLD.catalog_item_id
       OR NEW.catalog_item_version  IS DISTINCT FROM OLD.catalog_item_version
       OR NEW.quantity              IS DISTINCT FROM OLD.quantity
       OR NEW.amount_minor          IS DISTINCT FROM OLD.amount_minor
       OR NEW.currency              IS DISTINCT FROM OLD.currency
       OR NEW.action_fingerprint    IS DISTINCT FROM OLD.action_fingerprint
       OR NEW.created_at            IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'authorized transaction details are immutable; only execution state may change (transaction %)',
            OLD.id;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

COMMENT ON FUNCTION transactions_authorized_details_immutable() IS
    'Authorized transaction identity and purchase details are immutable after insert (including the primary key); only execution state (state, updated_at) may change.';

-- ---------------------------------------------------------------------------
-- Payment operation identity: immutable. Execution-state fields (status,
-- attempt_count, first_attempted_at, last_checked_at, updated_at) stay
-- mutable for the reconciliation machinery of later phases.
-- ---------------------------------------------------------------------------
CREATE FUNCTION payment_operations_identity_immutable() RETURNS trigger AS $$
BEGIN
    IF NEW.id                   IS DISTINCT FROM OLD.id
       OR NEW.transaction_id    IS DISTINCT FROM OLD.transaction_id
       OR NEW.operation_type    IS DISTINCT FROM OLD.operation_type
       OR NEW.paypal_request_id IS DISTINCT FROM OLD.paypal_request_id
       OR NEW.payload_fingerprint IS DISTINCT FROM OLD.payload_fingerprint
       OR NEW.created_at        IS DISTINCT FROM OLD.created_at
    THEN
        RAISE EXCEPTION 'payment operation identity is immutable (operation %)', OLD.id;
    END IF;

    -- provider_resource_id: NULL -> value is the one permitted transition
    -- (the provider result becomes known). Once set it must never change,
    -- including back to NULL.
    IF OLD.provider_resource_id IS NOT NULL
       AND NEW.provider_resource_id IS DISTINCT FROM OLD.provider_resource_id
    THEN
        RAISE EXCEPTION 'provider resource id cannot change once set (operation %)', OLD.id;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER payment_operations_identity_immutable
    BEFORE UPDATE ON payment_operations
    FOR EACH ROW EXECUTE FUNCTION payment_operations_identity_immutable();
