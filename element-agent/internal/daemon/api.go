package daemon

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"encoding/json/v2"
)

type API struct {
	base   string
	secret string
	hc     *http.Client
}

func NewAPI(base, secret string) *API {
	return &API{
		base:   strings.TrimSuffix(base, "/"),
		secret: secret,
		hc:     &http.Client{Timeout: 2 * time.Minute},
	}
}

type Reservation struct {
	UserID string `json:"user_id"`
	Claim  string `json:"claim"`
}

func (a *API) Reserve(ctx context.Context, name string) (Reservation, error) {
	var out Reservation
	err := a.post(ctx, "/reserve", map[string]any{"name": name}, &out)
	return out, err
}

func (a *API) Register(ctx context.Context, name, claim string) error {
	return a.post(ctx, "/register", map[string]any{"name": name, "claim": claim}, nil)
}

func (a *API) Deregister(ctx context.Context, name, claim string, release bool) error {
	return a.post(ctx, "/deregister", map[string]any{"name": name, "claim": claim, "release": release}, nil)
}

func (a *API) post(ctx context.Context, path string, payload map[string]any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func Reload() error {
	req, err := http.NewRequest(http.MethodPost, "http://"+LoopbackAddr+"/reload", nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return nil
}
