package paypal

// Link is one HATEOAS link from a PayPal response.
type Link struct {
	Href   string `json:"href"`
	Rel    string `json:"rel"`
	Method string `json:"method"`
}

// Money is a PayPal monetary amount: a decimal string plus currency code, as
// the API contract defines it. Waypoint keeps integer minor units internally;
// conversion happens at this boundary only.
type Money struct {
	CurrencyCode string `json:"currency_code"`
	Value        string `json:"value"`
}

// Capture is one captured payment as reported by the provider.
type Capture struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Amount       Money  `json:"amount"`
	FinalCapture bool   `json:"final_capture"`
	Links        []Link `json:"links"`
}

// PaymentCollection groups the payment resources of a purchase unit.
type PaymentCollection struct {
	Captures []Capture `json:"captures"`
}

// PurchaseUnit is one purchase unit of an order.
type PurchaseUnit struct {
	ReferenceID string            `json:"reference_id"`
	Amount      Money             `json:"amount"`
	Payments    PaymentCollection `json:"payments"`
}

// Name is a payer name.
type Name struct {
	GivenName string `json:"given_name"`
	Surname   string `json:"surname"`
}

// Payer is the payer of an order, when the provider reports one.
type Payer struct {
	PayerID      string `json:"payer_id"`
	EmailAddress string `json:"email_address"`
	Name         Name   `json:"name"`
}

// Order is the Orders v2 resource as returned by create, get, and capture.
type Order struct {
	ID            string         `json:"id"`
	Status        string         `json:"status"`
	PurchaseUnits []PurchaseUnit `json:"purchase_units"`
	Payer         *Payer         `json:"payer,omitempty"`
	Links         []Link         `json:"links"`
}

// Order and capture statuses the spike reasons about (provider vocabulary).
const (
	OrderStatusCreated   = "CREATED"
	OrderStatusApproved  = "APPROVED"
	OrderStatusCompleted = "COMPLETED"
	OrderStatusVoided    = "VOIDED"

	CaptureStatusCompleted = "COMPLETED"
	CaptureStatusPending   = "PENDING"
)

// ApprovalURL returns the buyer-approval link. The current API returns the
// modern "payer-action" rel; older integrations used "approve". Both are
// accepted, payer-action first.
func (o *Order) ApprovalURL() string {
	for _, l := range o.Links {
		if l.Rel == "payer-action" && l.Href != "" {
			return l.Href
		}
	}
	for _, l := range o.Links {
		if l.Rel == "approve" && l.Href != "" {
			return l.Href
		}
	}
	return ""
}

// Captures flattens every capture carried by the order.
func (o *Order) Captures() []Capture {
	var out []Capture
	for _, pu := range o.PurchaseUnits {
		out = append(out, pu.Payments.Captures...)
	}
	return out
}
