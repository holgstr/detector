package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetMe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/getMe") {
			t.Errorf("path=%s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"id": 1, "username": "detector_bot", "first_name": "Detector"},
		})
	}))
	t.Cleanup(ts.Close)
	c := New("TEST")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	me, err := c.GetMe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if me.Username != "detector_bot" {
		t.Fatalf("%+v", me)
	}
}

func TestSendMessage(t *testing.T) {
	var gotBody []byte
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/botTEST/sendMessage") {
			t.Errorf("path=%s", r.URL.Path)
		}
		gotBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	t.Cleanup(ts.Close)

	c := New("TEST")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	if err := c.SendMessage(context.Background(), 99, "hello"); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["chat_id"].(float64) != 99 || body["text"] != "hello" {
		t.Fatalf("%s", gotBody)
	}
}

func TestGetUpdates(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "8" {
			t.Errorf("offset=%s", r.URL.Query().Get("offset"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true,
			"result": []map[string]any{
				{
					"update_id": 8,
					"message": map[string]any{
						"text": "/start",
						"chat": map[string]any{"id": 123, "type": "private"},
						"from": map[string]any{"id": 123, "username": "me"},
					},
				},
			},
		})
	}))
	t.Cleanup(ts.Close)

	c := New("TEST")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	up, err := c.GetUpdates(context.Background(), 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(up) != 1 || up[0].Message.Chat.ID != 123 || up[0].Message.Text != "/start" {
		t.Fatalf("%+v", up)
	}
}

func TestAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	t.Cleanup(ts.Close)
	c := New("bad")
	c.Base = ts.URL
	c.HTTP = ts.Client()
	if err := c.SendMessage(context.Background(), 1, "x"); err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("err=%v", err)
	}
}

func TestScrubToken(t *testing.T) {
	c := New("secret-token-value")
	err := c.scrub(fmt.Errorf("Get \"https://api.telegram.org/botsecret-token-value/getUpdates\": timeout"))
	if err == nil || strings.Contains(err.Error(), "secret-token-value") {
		t.Fatalf("leaked: %v", err)
	}
	if !strings.Contains(err.Error(), "REDACTED") {
		t.Fatalf("got %v", err)
	}
}
