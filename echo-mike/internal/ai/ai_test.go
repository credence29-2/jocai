package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeOllama(t *testing.T, models []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			var list []map[string]string
			for _, m := range models {
				list = append(list, map[string]string{"name": m})
			}
			json.NewEncoder(w).Encode(map[string]any{"models": list})
		case "/api/chat":
			var in struct {
				Model    string `json:"model"`
				Stream   bool   `json:"stream"`
				Messages []struct{ Role, Content string }
			}
			json.NewDecoder(r.Body).Decode(&in)
			if in.Model != "llama3.1:8b" {
				w.WriteHeader(404)
				w.Write([]byte(`{"error":"model '` + in.Model + `' not found"}`))
				return
			}
			if !in.Stream || len(in.Messages) != 2 || in.Messages[0].Role != "system" || !strings.Contains(in.Messages[1].Content, "Ready?") && !strings.Contains(in.Messages[1].Content, "Summarize") {
				t.Errorf("bad request: %+v", in)
			}
			for _, piece := range []string{"- ", "Pay the ", "invoice."} {
				json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": piece}, "done": false})
			}
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": ""}, "done": true})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestOllama(t *testing.T) {
	srv := fakeOllama(t, []string{"llama3.1:8b", "mistral:latest"})
	defer srv.Close()
	c := New(Config{Engine: EngineOllama, OllamaURL: srv.URL + "/"})
	var got []string
	sys, prompt := Summarize("Please pay invoice 42 by Friday.")
	all, err := c.Stream(context.Background(), sys, prompt, func(s string) { got = append(got, s) })
	if err != nil || all != "- Pay the invoice." || len(got) != 3 {
		t.Fatalf("stream: %q %v %v", all, got, err)
	}
	if line, err := c.Test(context.Background()); err != nil || !strings.HasPrefix(line, "llama3.1:8b answered in") {
		t.Fatalf("test: %q %v", line, err)
	}

	// A model that isn't pulled yet gets the pull command.
	c = New(Config{Engine: EngineOllama, OllamaURL: srv.URL, OllamaModel: "qwen2.5:14b"})
	if _, err := c.Test(context.Background()); err == nil || !strings.Contains(err.Error(), "ollama pull qwen2.5:14b") {
		t.Fatalf("missing model: %v", err)
	}
	// "mistral" matches the "mistral:latest" tag, but chat then 404s.
	c = New(Config{Engine: EngineOllama, OllamaURL: srv.URL, OllamaModel: "mistral"})
	if err := c.ollamaHasModel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stream(context.Background(), "s", "p", nil); err == nil || !strings.Contains(err.Error(), "ollama pull mistral") {
		t.Fatalf("chat 404: %v", err)
	}

	// Nothing listening.
	srv.Close()
	if _, err := c.Test(context.Background()); err == nil || !strings.Contains(err.Error(), "Is it running") {
		t.Fatalf("down: %v", err)
	}
}

func TestAnthropic(t *testing.T) {
	var gotKey, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		var in struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			System []struct{ Text string }
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotModel = in.Model
		if gotKey == "bad" {
			w.WriteHeader(401)
			w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		if !in.Stream || len(in.System) != 1 {
			t.Errorf("bad request: %+v", in)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, ev := range []string{
			`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"m","stop_reason":null,"usage":{"input_tokens":1,"output_tokens":1}}}`,
			`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hi "}}`,
			`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Maria,"}}`,
			`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}`,
			`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`,
			`event: message_stop` + "\n" + `data: {"type":"message_stop"}`,
		} {
			w.Write([]byte(ev + "\n\n"))
		}
	}))
	defer srv.Close()
	c := New(Config{Engine: EngineAnthropic, AnthropicKey: " sk-test ", AnthropicURL: srv.URL})
	var got []string
	sys, prompt := DraftReply("yes, Thursday works", "Can we meet Thursday?", "Jacob")
	all, err := c.Stream(context.Background(), sys, prompt, func(s string) { got = append(got, s) })
	if err != nil || all != "Hi Maria," || len(got) != 2 || gotKey != "sk-test" || gotModel != DefaultAnthropicModel {
		t.Fatalf("stream: %q %v key=%q model=%q %v", all, got, gotKey, gotModel, err)
	}
	if line, err := c.Test(context.Background()); err != nil || !strings.Contains(line, `"Hi Maria,"`) {
		t.Fatalf("test: %q %v", line, err)
	}
	c = New(Config{Engine: EngineAnthropic, AnthropicKey: "bad", AnthropicURL: srv.URL})
	if _, err := c.Stream(context.Background(), "s", "p", nil); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("401: %v", err)
	}
	if _, err := New(Config{Engine: EngineAnthropic}).Test(context.Background()); err == nil || !strings.Contains(err.Error(), "paste your Anthropic API key") {
		t.Fatalf("no key: %v", err)
	}
	if _, err := New(Config{}).Stream(context.Background(), "s", "p", nil); err != ErrOff {
		t.Fatalf("off: %v", err)
	}
}

func TestPrompts(t *testing.T) {
	if _, _, ok := Rewrite("louder", "x"); ok {
		t.Fatal("unknown mode accepted")
	}
	for _, m := range Modes {
		sys, p, ok := Rewrite(m.ID, "pls fix teh printer")
		if !ok || !strings.Contains(p, "pls fix teh printer") || sys == "" {
			t.Fatalf("%s: %q", m.ID, p)
		}
	}
	long := strings.Repeat("a", maxChars+100)
	if _, p := Summarize(long); !strings.Contains(p, "cut for length") || len(p) > maxChars+500 {
		t.Fatal("long mail not clipped")
	}
	if c := (Config{Engine: EngineOllama}).Model(); c != DefaultOllamaModel {
		t.Fatal(c)
	}
}

func TestOpenAI(t *testing.T) {
	var gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			w.WriteHeader(404)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		var in struct {
			Model    string `json:"model"`
			Stream   bool   `json:"stream"`
			Messages []struct{ Role string }
		}
		json.NewDecoder(r.Body).Decode(&in)
		gotModel = in.Model
		if gotAuth == "Bearer bad" {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"Incorrect API key provided"}}`))
			return
		}
		if !in.Stream || len(in.Messages) != 2 || in.Messages[0].Role != "system" {
			t.Errorf("bad request: %+v", in)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, piece := range []string{"Hi ", "Derek,"} {
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"` + piece + `"}}]}` + "\n\n"))
		}
		w.Write([]byte("data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()
	c := New(Config{Engine: EngineOpenAI, OpenAIKey: " sk-work ", OpenAIURL: srv.URL + "/"})
	var got []string
	all, err := c.Stream(context.Background(), "s", "p", func(s string) { got = append(got, s) })
	if err != nil || all != "Hi Derek," || len(got) != 2 || gotAuth != "Bearer sk-work" || gotModel != DefaultOpenAIModel {
		t.Fatalf("stream: %q %v auth=%q model=%q %v", all, got, gotAuth, gotModel, err)
	}
	if line, err := c.Test(context.Background()); err != nil || !strings.HasPrefix(line, "gpt-5-mini answered") {
		t.Fatalf("test: %q %v", line, err)
	}
	c = New(Config{Engine: EngineOpenAI, OpenAIKey: "bad", OpenAIURL: srv.URL})
	if _, err := c.Stream(context.Background(), "s", "p", nil); err == nil || !strings.Contains(err.Error(), "rejected the API key") {
		t.Fatalf("401: %v", err)
	}
	if _, err := New(Config{Engine: EngineOpenAI}).Test(context.Background()); err == nil || !strings.Contains(err.Error(), "OpenAI API key") {
		t.Fatalf("no key: %v", err)
	}

	// Per-account choices.
	base := Config{Engine: EngineOllama}
	if base.ForAccount("").Engine != EngineOllama || base.ForAccount("off").Engine != EngineOff || base.ForAccount(EngineOpenAI).Engine != EngineOpenAI {
		t.Fatal("ForAccount")
	}
}
