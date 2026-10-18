-- M2-P1: capture-attempt ownership via a time-bounded lease.
--
-- A capture attempt's IN_FLIGHT state can be inspected by a DIFFERENT worker
-- than the one that began it (restart, operator, another process). Without
-- ownership, a recovery worker could demote a genuinely active attempt and
-- race the live mutation. The lease makes "abandoned" decidable:
--   * begin/takeover writes owner_id + lease_expires_at = now() + TTL
--     (TTL comfortably exceeds the provider HTTP timeout);
--   * a demotion or takeover is only allowed once the lease has expired
--     (or was never set, i.e. legacy/crashed rows).
--
-- Additive and safe to apply over existing rows (no backfill needed).
ALTER TABLE merchant_operations
  ADD COLUMN IF NOT EXISTS owner_id text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS lease_expires_at timestamptz;
