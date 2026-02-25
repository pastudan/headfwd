package headscale

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type Client struct {
	BaseURL    string
	APIKey     string
	httpClient *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

type User struct {
	Name      string `json:"name"`
	ID        string `json:"id,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

func (u User) NumericID() (uint64, error) {
	if u.ID == "" {
		return 0, fmt.Errorf("user has no ID")
	}
	return strconv.ParseUint(u.ID, 10, 64)
}

type Node struct {
	ID          string   `json:"id"`
	GivenName   string   `json:"givenName"`
	Name        string   `json:"name"`
	IPAddresses []string `json:"ipAddresses"`
	Online      bool     `json:"online"`
	LastSeen    string   `json:"lastSeen"`
	CreatedAt   string   `json:"createdAt"`
	ForcedTags  []string `json:"forcedTags"`
	ValidTags   []string `json:"validTags"`
	User        User     `json:"user"`
}

type PreauthKey struct {
	Key        string `json:"key"`
	User       User   `json:"user"`
	Reusable   bool   `json:"reusable"`
	Ephemeral  bool   `json:"ephemeral"`
	Expiration string `json:"expiration"`
	CreatedAt  string `json:"createdAt,omitempty"`
}

type CreateKeyRequest struct {
	User       uint64 `json:"user"`
	Reusable   bool   `json:"reusable"`
	Ephemeral  bool   `json:"ephemeral"`
	Expiration string `json:"expiration"`
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("headscale API %s %s returned %d: %s", method, path, resp.StatusCode, string(data))
	}

	return data, nil
}

func (c *Client) ListUsers() ([]User, error) {
	data, err := c.do("GET", "/api/v1/user", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Users []User `json:"users"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode users: %w", err)
	}
	return result.Users, nil
}

func (c *Client) ListNodes() ([]Node, error) {
	data, err := c.do("GET", "/api/v1/node", nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		Nodes []Node `json:"nodes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode nodes: %w", err)
	}
	return result.Nodes, nil
}

func (c *Client) GetUserByName(name string) (*User, error) {
	users, err := c.ListUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Name == name {
			return &u, nil
		}
	}
	return nil, fmt.Errorf("user %q not found", name)
}

func (c *Client) CreateUser(name string) (*User, error) {
	data, err := c.do("POST", "/api/v1/user", map[string]string{"name": name})
	if err != nil {
		return nil, err
	}

	var result struct {
		User User `json:"user"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode user: %w", err)
	}
	return &result.User, nil
}

func (c *Client) DeleteUser(name string) error {
	_, err := c.do("DELETE", "/api/v1/user/"+name, nil)
	return err
}

func (c *Client) DeleteNode(id string) error {
	_, err := c.do("DELETE", "/api/v1/node/"+id, nil)
	return err
}

func (c *Client) CreatePreauthKey(req CreateKeyRequest) (*PreauthKey, error) {
	data, err := c.do("POST", "/api/v1/preauthkey", req)
	if err != nil {
		return nil, err
	}

	var result struct {
		PreAuthKey PreauthKey `json:"preAuthKey"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decode preauth key: %w", err)
	}
	return &result.PreAuthKey, nil
}
