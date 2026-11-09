package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubPhone mimics the phone API behind a relay-style prefix check:
// every request must arrive under /r/tok123/ (stripped before routing),
// and /api/* calls need the session header issued by unlock.
type stubPhone struct {
	session   string
	unlockN   int
	lastCmd   map[string]any
	expireOne bool
}

func (s *stubPhone) handler(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/r/tok123/") {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/r/tok123")
	w.Header().Set("Content-Type", "application/json")
	switch path {
	case "/api/unlock":
		var body struct {
			PIN  string `json:"pin"`
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.PIN != "1234" {
			http.Error(w, `{"error":"wrong PIN"}`, http.StatusUnauthorized)
			return
		}
		s.unlockN++
		s.session = fmt.Sprintf("sess%d", s.unlockN)
		fmt.Fprintf(w, `{"token":%q,"name":%q}`, s.session, body.Name)
	case "/api/state":
		if r.Header.Get("X-Remote-Token") != s.session {
			http.Error(w, `{"error":"locked"}`, http.StatusUnauthorized)
			return
		}
		if s.expireOne {
			s.expireOne = false
			s.session = "stale"
			http.Error(w, `{"error":"locked"}`, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{
			"connected": true, "playing": true, "loading": false,
			"positionMs": 61000, "durationMs": 213000,
			"index": 1, "queueSize": 2, "shuffle": false, "repeat": 1,
			"hasNext": true, "hasPrevious": true,
			"volume": 9, "volumeMax": 15,
			"current": {"videoId":"v2","title":"Second Song","artist":"Artist B","thumbnailUrl":null,"durationText":"3:33"},
			"queue": [
				{"videoId":"v1","title":"First Song","artist":"Artist A","thumbnailUrl":"http://x/y.jpg","durationText":"3:01","queuedBy":"Irene"},
				{"videoId":"v2","title":"Second Song","artist":"Artist B","thumbnailUrl":null,"durationText":"3:33"}
			],
			"users": ["Irene"]
		}`)
	case "/api/command":
		if r.Header.Get("X-Remote-Token") != s.session {
			http.Error(w, `{"error":"locked"}`, http.StatusUnauthorized)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&s.lastCmd)
		fmt.Fprint(w, `{"ok":true}`)
	case "/api/lyrics":
		if r.Header.Get("X-Remote-Token") != s.session {
			http.Error(w, `{"error":"locked"}`, http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"videoId":"v2","found":true,"synced":true,"source":"LRCLIB",
			"lines":[{"at":0,"text":"line one"},{"at":5000,"text":"line two"}]}`)
	default:
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}
}

func newStub(t *testing.T) (*stubPhone, *Client) {
	t.Helper()
	stub := &stubPhone{}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	t.Cleanup(srv.Close)
	return stub, New(Config{
		BaseURL: srv.URL, RelayToken: "tok123", PIN: "1234", Name: "Desktop",
	})
}

func TestStateOverRelay(t *testing.T) {
	_, c := newStub(t)
	st, err := c.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Playing || st.Index != 1 || len(st.Queue) != 2 {
		t.Fatalf("bad state: %+v", st)
	}
	if st.Current == nil || st.Current.VideoID != "v2" {
		t.Fatalf("bad current: %+v", st.Current)
	}
	if st.Queue[0].QueuedBy != "Irene" || st.Queue[0].ThumbnailURL == nil {
		t.Fatalf("bad queue[0]: %+v", st.Queue[0])
	}
	if st.Volume == nil || *st.Volume != 9 {
		t.Fatalf("bad volume: %+v", st.Volume)
	}
}

func TestCommandShapes(t *testing.T) {
	stub, c := newStub(t)
	ctx := context.Background()
	if err := c.Move(ctx, 3, 2, "vX"); err != nil {
		t.Fatal(err)
	}
	if stub.lastCmd["action"] != "move" ||
		stub.lastCmd["from"] != float64(3) ||
		stub.lastCmd["to"] != float64(2) ||
		stub.lastCmd["fromId"] != "vX" {
		t.Fatalf("bad move body: %v", stub.lastCmd)
	}
	if err := c.Remove(ctx, 1, "v2"); err != nil {
		t.Fatal(err)
	}
	if stub.lastCmd["videoId"] != "v2" || stub.lastCmd["index"] != float64(1) {
		t.Fatalf("bad remove body: %v", stub.lastCmd)
	}
	if err := c.SetVolume(ctx, 0.5); err != nil {
		t.Fatal(err)
	}
	if stub.lastCmd["level"] != 0.5 {
		t.Fatalf("bad volume body: %v", stub.lastCmd)
	}
}

func TestSessionExpiryTriggersReunlock(t *testing.T) {
	stub, c := newStub(t)
	ctx := context.Background()
	if _, err := c.State(ctx); err != nil {
		t.Fatal(err)
	}
	stub.expireOne = true
	if _, err := c.State(ctx); err != nil {
		t.Fatalf("expected re-unlock to recover, got %v", err)
	}
	if stub.unlockN != 2 {
		t.Fatalf("expected 2 unlocks, got %d", stub.unlockN)
	}
}

func TestLyricsParse(t *testing.T) {
	_, c := newStub(t)
	ly, err := c.Lyrics(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !ly.Found || !ly.Synced || len(ly.Lines) != 2 || ly.Lines[1].At != 5000 {
		t.Fatalf("bad lyrics: %+v", ly)
	}
}

func TestWrongPIN(t *testing.T) {
	stub := &stubPhone{}
	srv := httptest.NewServer(http.HandlerFunc(stub.handler))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, RelayToken: "tok123", PIN: "0000"})
	err := c.Connect(context.Background())
	if err == nil {
		t.Fatal("expected error for wrong PIN")
	}
	if !strings.Contains(err.Error(), "wrong PIN") {
		t.Fatalf("expected server message, got %v", err)
	}
}

func TestDirectModeHasNoPrefix(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			sawPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/unlock" {
				fmt.Fprint(w, `{"token":"s","name":"n"}`)
				return
			}
			fmt.Fprint(w, `{"connected":true,"queue":[]}`)
		}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, PIN: "1234"})
	if _, err := c.State(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(sawPath, "/r/") {
		t.Fatalf("direct mode must not use relay prefix, saw %s", sawPath)
	}
}
