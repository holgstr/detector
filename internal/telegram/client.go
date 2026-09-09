package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.telegram.org"

// Client is a tiny Bot API wrapper (sendMessage + getUpdates).
type Client struct {
	Token string
	HTTP  *http.Client
	Base  string // override in tests
}

func New(token string) *Client {
	return &Client{
		Token: strings.TrimSpace(token),
		HTTP:  &http.Client{Timeout: 90 * time.Second},
		Base:  apiBase,
	}
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

type Message struct {
	Text string `json:"text"`
	Chat Chat   `json:"chat"`
	From User   `json:"from"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type sendReq struct {
	ChatID                int64  `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

// GetMe checks the token and returns the bot's identity.
func (c *Client) GetMe(ctx context.Context) (User, error) {
	var me User
	if err := c.do(ctx, "getMe", nil, "", &me); err != nil {
		return User{}, err
	}
	return me, nil
}

// SendMessage posts a plain-text DM.
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string) error {
	body, err := json.Marshal(sendReq{
		ChatID:                chatID,
		Text:                  text,
		DisableWebPagePreview: true,
	})
	if err != nil {
		return err
	}
	var unused json.RawMessage
	return c.do(ctx, "sendMessage", bytes.NewReader(body), "application/json", &unused)
}

// GetUpdates long-polls for inbound messages. timeoutSec=0 is a single non-blocking fetch.
func (c *Client) GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]Update, error) {
	q := url.Values{}
	if offset > 0 {
		q.Set("offset", strconv.FormatInt(offset, 10))
	}
	if timeoutSec > 0 {
		q.Set("timeout", strconv.Itoa(timeoutSec))
	}
	path := "getUpdates"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	var updates []Update
	if err := c.do(ctx, path, nil, "", &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

func (c *Client) do(ctx context.Context, method string, body io.Reader, contentType string, dest any) error {
	if c.Token == "" {
		return fmt.Errorf("telegram bot token is empty")
	}
	base := c.Base
	if base == "" {
		base = apiBase
	}
	u := strings.TrimRight(base, "/") + "/bot" + c.Token + "/" + method
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	}
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")

	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return c.scrub(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	var api apiResponse
	if err := json.Unmarshal(raw, &api); err != nil {
		return fmt.Errorf("telegram decode %s: %w (%s)", method, err, resp.Status)
	}
	if !api.OK {
		desc := api.Description
		if desc == "" {
			desc = resp.Status
		}
		return fmt.Errorf("telegram %s: %s", method, desc)
	}
	if dest == nil || len(api.Result) == 0 || string(api.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(api.Result, dest); err != nil {
		return fmt.Errorf("telegram result %s: %w", method, err)
	}
	return nil
}

func (c *Client) scrub(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if tok := c.Token; tok != "" && strings.Contains(msg, tok) {
		msg = strings.ReplaceAll(msg, tok, "REDACTED")
	}
	if msg == err.Error() {
		return err
	}
	return fmt.Errorf("%s", msg)
}
