package matrix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encoding/json/jsontext"
	"encoding/json/v2"
)

const (
	MaxEventBytes  = 65536
	requestTimeout = 30 * time.Second
)

var lenientJSON = json.JoinOptions(jsontext.AllowInvalidUTF8(true), jsontext.AllowDuplicateNames(true))

type APIError struct {
	Status  int    `json:"-"`
	ErrCode string `json:"errcode"`
	Message string `json:"error"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("matrix %d %s: %s", e.Status, e.ErrCode, e.Message)
}

func ErrCode(err error) string {
	if apiErr, ok := errors.AsType[*APIError](err); ok {
		return apiErr.ErrCode
	}
	return ""
}

type Client struct {
	base       string
	serverName string
	asToken    string
	adminToken string
	hc         *http.Client
}

func New(base, serverName, asToken, adminToken string) *Client {
	return &Client{
		base:       strings.TrimSuffix(base, "/"),
		serverName: serverName,
		asToken:    asToken,
		adminToken: adminToken,
		hc: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (c *Client) ServerName() string { return c.serverName }

func (c *Client) UserID(name string) string {
	return "@agent_" + name + ":" + c.serverName
}

func (c *Client) do(ctx context.Context, method, path, token string, query url.Values, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}

	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{Status: resp.StatusCode}
		if err := json.Unmarshal(data, apiErr, lenientJSON); err != nil {
			apiErr.Message = strings.TrimSpace(string(data))
		}
		return apiErr
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out, lenientJSON)
}

func asUser(userID string) url.Values {
	return url.Values{"user_id": {userID}}
}
