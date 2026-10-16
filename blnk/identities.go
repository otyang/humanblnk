package blnk

import (
	"context"
	"net/http"
)

// CreateIdentity creates an identity (POST /identities).
func (c *Client) CreateIdentity(ctx context.Context, id Identity) (*Identity, error) {
	var out Identity
	return &out, c.do(ctx, http.MethodPost, "/identities", id, &out)
}

// GetIdentity fetches an identity by id.
func (c *Client) GetIdentity(ctx context.Context, id string) (*Identity, error) {
	var out Identity
	return &out, c.do(ctx, http.MethodGet, "/identities/"+esc(id), nil, &out)
}

// Identity is a Blnk identity: the person or organisation balances belong to.
type Identity struct {
	IdentityID       string   `json:"identity_id,omitempty"`
	IdentityType     string   `json:"identity_type"` // individual | organization
	OrganizationName string   `json:"organization_name,omitempty"`
	Category         string   `json:"category,omitempty"`
	FirstName        string   `json:"first_name,omitempty"`
	LastName         string   `json:"last_name,omitempty"`
	OtherNames       string   `json:"other_names,omitempty"`
	Gender           string   `json:"gender,omitempty"`
	EmailAddress     string   `json:"email_address,omitempty"`
	PhoneNumber      string   `json:"phone_number,omitempty"`
	Nationality      string   `json:"nationality,omitempty"`
	Street           string   `json:"street,omitempty"`
	Country          string   `json:"country,omitempty"`
	State            string   `json:"state,omitempty"`
	PostCode         string   `json:"post_code,omitempty"`
	City             string   `json:"city,omitempty"`
	MetaData         MetaData `json:"meta_data,omitempty"`
}
