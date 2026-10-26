package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

func TestAIRun(t *testing.T) {
	var gotPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[{"name":"llama3.1:8b"}]}`))
			return
		}
		var in struct {
			Messages []struct{ Content string }
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotPrompt = in.Messages[1].Content
		for _, s := range []string{"- Maria ", "asked twice."} {
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": s}})
		}
		json.NewEncoder(w).Encode(map[string]any{"done": true})
	}))
	defer srv.Close()
	hs := newHarness(t)

	// Off by default.
	var out map[string]string
	if code := hs.call("POST", "/api/ai/run", map[string]any{"task": "rewrite", "mode": "shorter", "text": "hello there"}, &out); code != 200 || !strings.Contains(out["error"], "turned off") {
		t.Fatalf("off: %d %v", code, out)
	}

	// Settings keep the key masked and the test endpoint works.
	st := store.Settings{}
	st.Engine = "ollama"
	st.OllamaURL = srv.URL
	st.AnthropicKey = "sk-secret"
	if code := hs.call("POST", "/api/settings", st, nil); code != 200 {
		t.Fatal(code)
	}
	var state stateOut
	hs.call("GET", "/api/state", nil, &state)
	if state.Settings.AnthropicKey != "saved" || state.Settings.Engine != "ollama" {
		t.Fatalf("state %+v", state.Settings)
	}
	st.AnthropicKey = "saved"
	hs.call("POST", "/api/settings", st, nil)
	if hs.a.st.Get().Settings.AnthropicKey != "sk-secret" {
		t.Fatal("masked key overwrote the real one")
	}
	if code := hs.call("POST", "/api/ai/test", st, &out); code != 200 || !strings.HasPrefix(out["message"], "llama3.1:8b answered") {
		t.Fatalf("test: %d %v", code, out)
	}

	// Summarize a demo thread: the prompt carries the messages, the answer
	// comes back as JSON lines.
	var acct string
	for _, a := range state.Accounts {
		acct = a.ID
		break
	}
	rec := httptest.NewRecorder()
	body := `{"task":"summarize","accountId":"` + acct + `","threadId":"1"}`
	req := httptest.NewRequest("POST", "http://127.0.0.1:5555/api/ai/run", strings.NewReader(body))
	req.Header.Set("X-Echo-Token", hs.a.token)
	hs.h.ServeHTTP(rec, req)
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if rec.Code != 200 || len(lines) != 3 || lines[0] != `{"t":"- Maria "}` || lines[2] != `{"done":true}` {
		t.Fatalf("%d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(gotPrompt, "From: ") || !strings.Contains(gotPrompt, "-----") {
		t.Fatalf("prompt %q", gotPrompt)
	}
	// Draft needs a note.
	if code := hs.call("POST", "/api/ai/run", map[string]any{"task": "draft", "accountId": acct, "id": "1"}, &out); code != 500 || !strings.Contains(out["error"], "reply should cover") {
		t.Fatalf("draft without note: %d %v", code, out)
	}
}
