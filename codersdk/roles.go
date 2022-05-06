package codersdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

// ListSiteRoles lists all available site wide roles.
// This is not user specific.
func (c *Client) ListSiteRoles(ctx context.Context) ([]string, error) {
	res, err := c.request(ctx, http.MethodGet, "/api/v2/users/roles", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readBodyAsError(res)
	}
	var roles []string
	return roles, json.NewDecoder(res.Body).Decode(&roles)
}

// ListOrganizationRoles lists all available roles for a given organization.
// This is not user specific.
func (c *Client) ListOrganizationRoles(ctx context.Context, org uuid.UUID) ([]string, error) {
	res, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/api/v2/organizations/%s/members/roles/", org.String()), nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, readBodyAsError(res)
	}
	var roles []string
	return roles, json.NewDecoder(res.Body).Decode(&roles)
}