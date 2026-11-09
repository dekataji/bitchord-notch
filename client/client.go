// Package client speaks to a BitChord phone's web-remote API, either
// directly on the LAN (http://<phone>:8899) or through the VPS relay
// (https://remote.gaspol.men with the /r/<token>/ path prefix on every
// request — the relay strips the prefix and forwards; no cookie
// handling is needed when the prefix is always present).
//
// The wire contract mirrors the phone's RemoteBridge:
//
//	POST /api/unlock  {pin, name}            -> {token, name}
//	GET  /api/state                          -> player snapshot
//	POST /api/command {action, ...}          -> {ok}
//	GET  /api/lyrics                         -> synced lyrics for the
//	                                            current song
//
// Every /api/* call after unlock carries the session token in the
// X-Remote-Token header.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Config is persisted next to the executable (portable install).
type Config struct {
	BaseURL    string `json:"baseUrl"`    // relay origin or phone LAN address
	RelayToken string `json:"relayToken"` // empty = direct LAN mode
	PIN        string `json:"pin"`
	Name       string `json:"name"` // label shown in the phone's user list
}

func (c Config) Ready() bool {
	return strings.TrimSpace(c.BaseURL) != "" && strings.TrimSpace(c.PIN) != ""
}

type Song struct {
	VideoID      string  `json:"videoId"`
	Title        string  `json:"title"`
	Artist       string  `json:"artist"`
	ThumbnailURL *string `json:"thumbnailUrl"`
	DurationText *string `json:"durationText"`
	QueuedBy     string  `json:"queuedBy"`
}

type State struct {
	Connected   bool     `json:"connected"`
	Playing     bool     `json:"playing"`
	Loading     bool     `json:"loading"`
	PositionMs  int64    `json:"positionMs"`
	DurationMs  int64    `json:"durationMs"`
	Index       int      `json:"index"`
	QueueSize   int      `json:"queueSize"`
	Shuffle     bool     `json:"shuffle"`
	Repeat      int      `json:"repeat"` // 0 off, 1 all, 2 one
	HasNext     bool     `json:"hasNext"`
	HasPrevious bool     `json:"hasPrevious"`
	Volume      *int     `json:"volume"`
	VolumeMax   *int     `json:"volumeMax"`
	Current     *Song    `json:"current"`
	Queue       []Song   `json:"queue"`
	Users       []string `json:"users"`
}

type LyriJuniper struct {
	At   int64  `json:"at"`
	Text string `json:"text"`
}

type Lyrics struct {
	VideoID string        `json:"videoId"`
	Found   bool          `json:"found"`
	Pending bool          `json:"pending"`
	Synced  bool          `json:"synced"`
	Source  string        `json:"source"`
	Lines   []LyriJuniper `json:"lines"`
}

// APIError is a non-2xx answer from the phone or the relay.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("http %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("http %d", e.Status)
}

type Client struct {
	cfg     Config
	http    *http.Client
	mu      sync.Mutex
	session string
}

func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Name == "" {
		cfg.Name = "Desktop"
	}
	return &Client{
		cfg: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) Config() Config { return c.cfg }

func (c *Client) SetConfig(cfg Config) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.Name == "" {
		cfg.Name = "Desktop"
	}
	c.cfg = cfg
	c.session = "" // force re-unlock against the new target
}

func (c *Client) prefix() string {
	if c.cfg.RelayToken == "" {
		return ""
	}
	return "/r/" + c.cfg.RelayToken
}

// Connect performs the PIN unlock. Safe to call repeatedly; other
// methods call it lazily when there is no session yet.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.unlockLocked(ctx)
}

func (c *Client) unlockLocked(ctx context.Context) error {
	if c.cfg.PIN == "" {
		return fmt.Errorf("no PIN configured")
	}
	body, _ := json.Marshal(map[string]string{
		"pin":  c.cfg.PIN,
		"name": c.cfg.Name,
	})
	status, data, err := c.rawLocked(ctx, http.MethodPost, "/api/unlock", body, false)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return apiErr(status, data)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Token == "" {
		return fmt.Errorf("unlock: unexpected response")
	}
	c.session = out.Token
	return nil
}

// rawLocked performs one HTTP request. Caller holds c.mu.
func (c *Client) rawLocked(
	ctx context.Context, method, path string, body []byte, auth bool,
) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(
		ctx, method, c.cfg.BaseURL+c.prefix()+path, rdr,
	)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && c.session != "" {
		req.Header.Set("X-Remote-Token", c.session)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, data, err
}

// do performs an authenticated API call, unlocking (or re-unlocking
// after a 401) as needed.
func (c *Client) do(
	ctx context.Context, method, path string, body []byte,
) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == "" {
		if err := c.unlockLocked(ctx); err != nil {
			return nil, err
		}
	}
	status, data, err := c.rawLocked(ctx, method, path, body, true)
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized {
		// Session expired or the phone restarted: unlock once more.
		if err := c.unlockLocked(ctx); err != nil {
			return nil, err
		}
		status, data, err = c.rawLocked(ctx, method, path, body, true)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, apiErr(status, data)
	}
	return data, nil
}

func apiErr(status int, data []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &e); err == nil && e.Error != "" {
		return &APIError{Status: status, Message: e.Error}
	}
	// The relay answers some failures (403/502) as plain text.
	msg := strings.TrimSpace(string(data))
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return &APIError{Status: status, Message: msg}
}

func (c *Client) State(ctx context.Context) (*State, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/state", nil)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("state: %w", err)
	}
	return &st, nil
}

func (c *Client) Lyrics(ctx context.Context) (*Lyrics, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/lyrics", nil)
	if err != nil {
		return nil, err
	}
	var ly Lyrics
	if err := json.Unmarshal(data, &ly); err != nil {
		return nil, fmt.Errorf("lyrics: %w", err)
	}
	return &ly, nil
}

// Command sends one player command. Params are merged over {action}.
func (c *Client) Command(
	ctx context.Context, action string, params map[string]any,
) error {
	body := map[string]any{"action": action}
	for k, v := range params {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	data, err := c.do(ctx, http.MethodPost, "/api/command", raw)
	if err != nil {
		return err
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(data, &out); err != nil || !out.OK {
		return fmt.Errorf("command %s rejected", action)
	}
	return nil
}

func (c *Client) Toggle(ctx context.Context) error { return c.Command(ctx, "toggle", nil) }
func (c *Client) Next(ctx context.Context) error   { return c.Command(ctx, "next", nil) }
func (c *Client) Prev(ctx context.Context) error   { return c.Command(ctx, "prev", nil) }

func (c *Client) Seek(ctx context.Context, ms int64) error {
	if ms < 0 {
		ms = 0
	}
	return c.Command(ctx, "seek", map[string]any{"ms": ms})
}

func (c *Client) PlayIndex(ctx context.Context, index int) error {
	return c.Command(ctx, "playIndex", map[string]any{"index": index})
}

func (c *Client) Move(ctx context.Context, from, to int, fromID string) error {
	return c.Command(ctx, "move", map[string]any{
		"from": from, "to": to, "fromId": fromID,
	})
}

func (c *Client) Remove(ctx context.Context, index int, videoID string) error {
	return c.Command(ctx, "remove", map[string]any{
		"index": index, "videoId": videoID,
	})
}

// SetVolume takes a 0..1 level, as the phone's API expects.
func (c *Client) SetVolume(ctx context.Context, level float64) error {
	if level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}
	return c.Command(ctx, "volume", map[string]any{"level": level})
}

func (c *Client) ToggleShuffle(ctx context.Context) error {
	return c.Command(ctx, "shuffle", nil)
}

func (c *Client) CycleRepeat(ctx context.Context) error {
	return c.Command(ctx, "repeat", nil)
}
