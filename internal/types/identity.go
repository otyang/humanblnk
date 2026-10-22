package types

import "time"

// Identity is the owner of a customer's accounts: a Blnk identity holding no
// personal data, only references such as your customer id in Meta.
type Identity struct {
	ID        string // "idt_…", the Owner of the customer's accounts
	Meta      Meta
	CreatedAt time.Time
}
