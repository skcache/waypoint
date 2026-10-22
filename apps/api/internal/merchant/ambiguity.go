package merchant

import (
	"errors"

	"github.com/skcache/waypoint/apps/api/internal/payments/paypal"
)

// ambiguityPolicy decides whether a failed provider mutation may still have
// taken effect. It is the one place the capture path asks that question, so a
// wrong answer here changes financial behavior, not just logging.
type ambiguityPolicy interface {
	Ambiguous(err error) bool
}

// canonicalAmbiguity is the correct policy: the provider's own classifier,
// which treats an unreadable 2xx response, a timeout, a 408/429 and any
// transport failure as "the mutation may have applied".
type canonicalAmbiguity struct{}

func (canonicalAmbiguity) Ambiguous(err error) bool { return paypal.AmbiguousOutcome(err) }

// legacyAmbiguity reproduces a historical integration shortcut: only provider
// 5xx responses were considered ambiguous, and anything else — including a
// 2xx whose body could not be read — was treated as a definite failure. That
// assumption is wrong for mutating calls: the capture may have applied even
// though the merchant never saw the response.
//
// The fixture wires this policy for the F2 scenario so the defect is in
// application logic, not in the fault injector (which lives in the scripted
// provider and stays active after the repair).
type legacyAmbiguity struct{}

func (legacyAmbiguity) Ambiguous(err error) bool {
	var apiErr *paypal.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500
	}
	return false
}
