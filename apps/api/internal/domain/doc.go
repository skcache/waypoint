// Package domain holds Waypoint's pure domain model: typed identifiers,
// monetary primitives, the transaction state machine, authorization outcome
// aggregation, and the deterministic authority invariants that must hold
// before any transaction may advance toward financial execution.
//
// Central invariant: no transaction may advance toward financial execution
// unless the exact immutable proposed action is bound to valid,
// human-confirmed authority and has passed the required authorization gates.
//
// Boundaries (locked product decisions):
//
//   - This package is pure: no database, network, clock, or framework
//     dependencies. Time is passed in explicitly so behavior is testable and
//     reproducible.
//   - Deterministic authorization only. Semantic (LLM) verification is a
//     separate gate — implemented in a later phase — whose classification
//     feeds AggregateDecision. A model verdict never substitutes for the
//     checks here, and these checks never stand in for the model's.
//   - Nothing here mutates persistent state or calls the payment provider.
//     Persistence and provider coordination are later-phase concerns; the
//     transition rules and invariants here are the contracts they honor.
//   - Creating a transaction does not freeze authority forever: every later
//     phase that authorizes execution MUST re-validate the authority at the
//     moment of execution (contracts can expire between creation and
//     execution).
package domain
