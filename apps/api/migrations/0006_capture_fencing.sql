-- M2 correction: fencing generation for capture operation transitions.
--
-- A lease timestamp alone is not a fencing token: a slow provider call can
-- outlive its lease, and a reconciler could then demote an operation that
-- another worker has ALREADY taken over and is actively capturing. Every
-- capture claim/takeover increments this generation, and every worker-made
-- transition is conditioned on the generation the worker actually inspected.
-- A stale worker's conditional UPDATE then matches zero rows and is refused.
--
-- Additive; existing rows start at generation 1.
ALTER TABLE merchant_operations
  ADD COLUMN IF NOT EXISTS fence bigint NOT NULL DEFAULT 1;
