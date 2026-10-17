package postgres_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/skcache/waypoint/apps/api/internal/domain"
)

// TestDomainStatesMatchDatabaseConstraint keeps the domain state vocabulary
// and the database CHECK constraint in lockstep in both directions:
//
//  1. every state the domain knows must appear in the live constraint
//     definition, and every state the constraint allows must be a domain
//     state (a mismatch means a migration and the domain model have drifted);
//  2. functionally, a real row must accept each domain state; and
//  3. a state outside the vocabulary must be refused by the database.
func TestDomainStatesMatchDatabaseConstraint(t *testing.T) {
	pool := testPool(t)
	resetSchema(t, pool)
	ctx := context.Background()

	var def string
	if err := pool.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'transactions_state_valid'`).Scan(&def); err != nil {
		t.Fatalf("read transactions_state_valid constraint: %v", err)
	}
	re := regexp.MustCompile(`'([A-Z_]+)'::text`)
	matches := re.FindAllStringSubmatch(def, -1)
	if len(matches) == 0 {
		t.Fatalf("could not parse any states out of constraint definition %q", def)
	}
	dbStates := make(map[string]bool, len(matches))
	for _, m := range matches {
		dbStates[m[1]] = true
	}
	domainStates := domain.States()
	if len(dbStates) != len(domainStates) {
		t.Errorf("state count mismatch: database allows %d states, domain knows %d", len(dbStates), len(domainStates))
	}
	for _, s := range domainStates {
		if !dbStates[string(s)] {
			t.Errorf("domain state %s is missing from the database constraint", s)
		}
	}
	for s := range dbStates {
		found := false
		for _, d := range domainStates {
			if string(d) == s {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("database state %s is missing from the domain vocabulary", s)
		}
	}

	// Functional half: a real transaction row must accept every domain state.
	session := seedSession(t, pool)
	contract := seedIntentDraft(t, pool, session)
	confirmContract(t, pool, contract, 1)
	seedCatalogItem(t, pool, "keyboard-office-quiet")
	txn := seedTransaction(t, pool, session, contract, 1, "keyboard-office-quiet")

	for _, s := range domainStates {
		if _, err := pool.Exec(ctx, `UPDATE transactions SET state = $1 WHERE id = $2`, string(s), txn); err != nil {
			t.Errorf("database rejected domain state %s: %v", s, err)
		}
	}

	// And a state outside the vocabulary must be refused (check_violation).
	_, err := pool.Exec(ctx, `UPDATE transactions SET state = 'TELEPORTED' WHERE id = $1`, txn)
	wantPgError(t, err, "23514")
}
