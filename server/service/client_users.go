package service

import (
	"encoding/json"
	"fmt"
	"github.com/fleetdm/fleet/server/kolide"
	"github.com/pkg/errors"
	"net/http"
)

// CreateUser creates a new user, skipping the invitation process.
func (c *Client) CreateUser(p kolide.UserPayload) error {
	verb, path := "POST", "/api/v1/kolide/users/admin"
	response, err := c.AuthenticatedDo(verb, path, "", p)
	if err != nil {
		return errors.Wrapf(err, "%s %s", verb, path)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return errors.Errorf(
			"create user received status %d %s",
			response.StatusCode,
			extractServerErrorText(response.Body),
		)
	}

	var responseBody createUserResponse
	err = json.NewDecoder(response.Body).Decode(&responseBody)
	if err != nil {
		return errors.Wrap(err, "decode create user response")
	}

	if responseBody.Err != nil {
		return errors.Errorf("create user: %s", responseBody.Err)
	}

	return nil
}

// GetUser retrieves information about a user
func (c *Client) GetUser(id uint) (*kolide.User, error) {
	verb, path := "GET", fmt.Sprintf("/api/v1/kolide/users/%d", id)
	response, err := c.AuthenticatedDo(verb, path, "", nil)
	if err != nil {
		return nil, errors.Wrap(err, "GET /api/v1/kolide/users")
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusNotFound:
		return nil, notFoundErr{}
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.Errorf(
			"get user received status %d %s",
			response.StatusCode,
			extractServerErrorText(response.Body),
		)
	}

	var responseBody getUserResponse
	err = json.NewDecoder(response.Body).Decode(&responseBody)
	if err != nil {
		return nil, errors.Wrap(err, "decode get user response")
	}

	if responseBody.Err != nil {
		return nil, errors.Errorf("get user: %s", responseBody.Err)
	}

	return responseBody.User, nil
}

// ListUsers retrieves the list of all Users.
func (c *Client) ListUsers() ([]kolide.User, error) {
	response, err := c.AuthenticatedDo("GET", "/api/v1/kolide/users", "", nil)
	if err != nil {
		return nil, errors.Wrap(err, "GET /api/v1/kolide/users")
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, errors.Errorf(
			"list users received status %d %s",
			response.StatusCode,
			extractServerErrorText(response.Body),
		)
	}

	var responseBody listUsersResponse
	err = json.NewDecoder(response.Body).Decode(&responseBody)
	if err != nil {
		return nil, errors.Wrap(err, "decode list users response")
	}

	if responseBody.Err != nil {
		return nil, errors.Errorf("list users: %s", responseBody.Err)
	}

	return responseBody.Users, nil
}
