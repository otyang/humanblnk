package blnk

import (
	"context"
	"net/http"
)

// Create creates an identity (POST /identities). Blnk generates IdentityID
// unless you set one ("idt_" + a UUID).
func (s *IdentityService) Create(ctx context.Context, id Identity) (*Identity, error) {
	var out Identity
	return &out, s.client.doOnce(ctx, http.MethodPost, "/identities", id, &out)
}

// Get fetches an identity by id.
func (s *IdentityService) Get(ctx context.Context, id string) (*Identity, error) {
	var out Identity
	return &out, s.client.do(ctx, http.MethodGet, "/identities/"+esc(id), nil, &out)
}

// List lists identities (GET /identities). Without o.Filters Blnk returns
// every identity and ignores Limit and Offset.
func (s *IdentityService) List(ctx context.Context, o ListOptions) ([]Identity, error) {
	return getList[Identity](ctx, s.client, o.path("/identities"))
}

// Filter runs POST /identities/filter.
func (s *IdentityService) Filter(ctx context.Context, r FilterRequest) ([]Identity, int, error) {
	return filterList[Identity](ctx, s.client, "/identities/filter", r)
}

// Update changes an identity (PUT /identities/{id}). Only the non-empty fields
// of id are written; the rest keep their values. A non-empty MetaData replaces
// the stored metadata (use Metadata.Update to merge keys instead). Blnk would
// also drop its own record of which fields are tokenized, so Update first
// reads the identity and carries that record over unless id.MetaData sets it.
func (s *IdentityService) Update(ctx context.Context, identityID string, id Identity) error {
	if len(id.MetaData) > 0 {
		if _, set := id.MetaData[tokenizedFieldsKey]; !set {
			cur, err := s.Get(ctx, identityID)
			if err != nil {
				return err
			}
			if tf, ok := cur.MetaData[tokenizedFieldsKey]; ok {
				md := make(MetaData, len(id.MetaData)+1)
				for k, v := range id.MetaData {
					md[k] = v
				}
				md[tokenizedFieldsKey] = tf
				id.MetaData = md
			}
		}
	}
	return s.client.do(ctx, http.MethodPut, "/identities/"+esc(identityID), id, nil)
}

// tokenizedFieldsKey is the metadata key where Blnk records tokenized fields.
const tokenizedFieldsKey = "tokenized_fields"

// Delete deletes an identity.
func (s *IdentityService) Delete(ctx context.Context, id string) error {
	return s.client.do(ctx, http.MethodDelete, "/identities/"+esc(id), nil, nil)
}

// Tokenize replaces the given PII fields of an identity (e.g. "first_name",
// "email_address") with tokens. Needs tokenization enabled on Core.
func (s *IdentityService) Tokenize(ctx context.Context, id string, fields ...string) error {
	return s.client.do(ctx, http.MethodPost, "/identities/"+esc(id)+"/tokenize", map[string][]string{"fields": fields}, nil)
}

// TokenizeField tokenizes one field.
func (s *IdentityService) TokenizeField(ctx context.Context, id, field string) error {
	return s.client.do(ctx, http.MethodPost, "/identities/"+esc(id)+"/tokenize/"+esc(field), nil, nil)
}

// Detokenize returns the original values of the given tokenized fields, or of
// every tokenized field when none are named. Nothing is changed on Blnk.
func (s *IdentityService) Detokenize(ctx context.Context, id string, fields ...string) (map[string]string, error) {
	if fields == nil {
		fields = []string{}
	}
	var out struct {
		Fields map[string]string `json:"fields"`
	}
	err := s.client.do(ctx, http.MethodPost, "/identities/"+esc(id)+"/detokenize", map[string][]string{"fields": fields}, &out)
	return out.Fields, err
}

// DetokenizeField returns the original value of one tokenized field.
func (s *IdentityService) DetokenizeField(ctx context.Context, id, field string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	err := s.client.do(ctx, http.MethodGet, "/identities/"+esc(id)+"/detokenize/"+esc(field), nil, &out)
	return out.Value, err
}

// TokenizedFields lists which fields of an identity are tokenized.
func (s *IdentityService) TokenizedFields(ctx context.Context, id string) ([]string, error) {
	var out struct {
		Fields []string `json:"tokenized_fields"`
	}
	err := s.client.do(ctx, http.MethodGet, "/identities/"+esc(id)+"/tokenized-fields", nil, &out)
	return out.Fields, err
}

// Identity is a Blnk identity: the person or organisation balances belong to.
type Identity struct {
	IdentityID       string   `json:"identity_id,omitempty"`
	IdentityType     string   `json:"identity_type,omitempty"` // individual | organization
	OrganizationName string   `json:"organization_name,omitempty"`
	Category         string   `json:"category,omitempty"`
	FirstName        string   `json:"first_name,omitempty"`
	LastName         string   `json:"last_name,omitempty"`
	OtherNames       string   `json:"other_names,omitempty"`
	Gender           string   `json:"gender,omitempty"`
	DOB              Time     `json:"dob,omitzero"`
	EmailAddress     string   `json:"email_address,omitempty"`
	PhoneNumber      string   `json:"phone_number,omitempty"`
	Nationality      string   `json:"nationality,omitempty"`
	Street           string   `json:"street,omitempty"`
	Country          string   `json:"country,omitempty"`
	State            string   `json:"state,omitempty"`
	PostCode         string   `json:"post_code,omitempty"`
	City             string   `json:"city,omitempty"`
	CreatedAt        Time     `json:"created_at,omitzero"`
	MetaData         MetaData `json:"meta_data,omitempty"`
}
