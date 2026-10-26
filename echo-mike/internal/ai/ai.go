// Package ai writes summaries, replies and rewrites with a language model.
// Two engines: Ollama running on this PC, or Anthropic's API with your own
// key. Nothing is sent to either until you click an AI button.
package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const (
	EngineOff       = ""
	EngineOllama    = "ollama"
	EngineAnthropic = "anthropic"

	DefaultOllamaURL      = "http://127.0.0.1:11434"
	DefaultOllamaModel    = "llama3.1:8b"
	DefaultAnthropicModel = "claude-opus-5-5"
)

// Config is the AI part of Settings.
type Config struct {
	Engine         string `json:"aiEngine"`
	OllamaURL      string `json:"ollamaUrl"`
	OllamaModel    string `json:"ollamaModel"`
	AnthropicKey   string `json:"anthropicKey"`
	AnthropicModel string `json:"anthropicModel"`
	// AnthropicURL overrides the API host, for tests.
	AnthropicURL string `json:"-"`
}

// Defaults fills in blanks so a half-filled config still works.
func (c Config) Defaults() Config {
	if c.OllamaURL == "" {
		c.OllamaURL = DefaultOllamaURL
	}
	if c.OllamaModel == "" {
		c.OllamaModel = DefaultOllamaModel
	}
	if c.AnthropicModel == "" {
		c.AnthropicModel = DefaultAnthropicModel
	}
	c.OllamaURL = strings.TrimRight(strings.TrimSpace(c.OllamaURL), "/")
	return c
}

// On reports whether an engine is chosen.
func (c Config) On() bool { return c.Engine != EngineOff }

// Model is the model name the chosen engine will use.
func (c Config) Model() string {
	c = c.Defaults()
	switch c.Engine {
	case EngineOllama:
		return c.OllamaModel
	case EngineAnthropic:
		return c.AnthropicModel
	}
	return ""
}

// Client talks to the chosen engine.
type Client struct {
	cfg  Config
	http *http.Client
}

// New makes a client for the config. Model calls can run for a while on a
// local GPU, so the timeout is generous.
func New(cfg Config) *Client {
	return &Client{cfg: cfg.Defaults(), http: &http.Client{Timeout: 5 * time.Minute}}
}

// ErrOff is returned when no engine is chosen.
var ErrOff = errors.New("AI is turned off. Pick an engine under Settings > AI")

// Stream asks the model and hands back text as it arrives. It returns the
// whole answer when done.
func (c *Client) Stream(ctx context.Context, system, prompt string, out func(string)) (string, error) {
	if out == nil {
		out = func(string) {}
	}
	switch c.cfg.Engine {
	case EngineOllama:
		return c.ollama(ctx, system, prompt, out)
	case EngineAnthropic:
		return c.anthropic(ctx, system, prompt, out)
	}
	return "", ErrOff
}

// Test checks the engine can be reached and the model answers. It returns
// a short line to show the user.
func (c *Client) Test(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	switch c.cfg.Engine {
	case EngineOllama:
		if err := c.ollamaHasModel(ctx); err != nil {
			return "", err
		}
	case EngineAnthropic:
		if strings.TrimSpace(c.cfg.AnthropicKey) == "" {
			return "", errors.New("paste your Anthropic API key first")
		}
	default:
		return "", ErrOff
	}
	start := time.Now()
	ans, err := c.Stream(ctx, "Answer with the single word OK.", "Ready?", nil)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s answered in %.1fs (%q)", c.cfg.Model(), time.Since(start).Seconds(), strings.TrimSpace(ans)), nil
}

// Ollama: POST /api/chat streams one JSON object per line.

func (c *Client) ollama(ctx context.Context, system, prompt string, out func(string)) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":  c.cfg.OllamaModel,
		"stream": true,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.cfg.OllamaURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ollamaDown(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(b, &e)
		if strings.Contains(e.Error, "not found") {
			return "", fmt.Errorf("Ollama doesn't have the model %q. In a terminal, run: ollama pull %s", c.cfg.OllamaModel, c.cfg.OllamaModel)
		}
		if e.Error == "" {
			e.Error = strings.TrimSpace(string(b))
		}
		return "", fmt.Errorf("Ollama: %s", e.Error)
	}
	var all strings.Builder
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var chunk struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return all.String(), fmt.Errorf("Ollama: %s", chunk.Error)
		}
		if chunk.Message.Content != "" {
			all.WriteString(chunk.Message.Content)
			out(chunk.Message.Content)
		}
		if chunk.Done {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return all.String(), err
	}
	return all.String(), nil
}

// ollamaHasModel checks Ollama is running and the model is pulled.
func (c *Client) ollamaHasModel(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.cfg.OllamaURL+"/api/tags", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return ollamaDown(err)
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return fmt.Errorf("%s doesn't look like Ollama (%v)", c.cfg.OllamaURL, err)
	}
	want := c.cfg.OllamaModel
	for _, m := range tags.Models {
		// "llama3.1:8b" is listed as itself; "llama3.1" is listed as "llama3.1:latest".
		if m.Name == want || m.Name == want+":latest" || strings.TrimSuffix(m.Name, ":latest") == want {
			return nil
		}
	}
	return fmt.Errorf("Ollama is running but doesn't have %q yet. In a terminal, run: ollama pull %s", want, want)
}

func ollamaDown(err error) error {
	return fmt.Errorf("can't reach Ollama. Is it running? Install it from ollama.com, then try again (%v)", err)
}

// Anthropic, through the official SDK.

func (c *Client) anthropic(ctx context.Context, system, prompt string, out func(string)) (string, error) {
	opts := []option.RequestOption{option.WithAPIKey(strings.TrimSpace(c.cfg.AnthropicKey)), option.WithHTTPClient(c.http), option.WithMaxRetries(2)}
	if c.cfg.AnthropicURL != "" {
		opts = append(opts, option.WithBaseURL(c.cfg.AnthropicURL))
	}
	client := anthropic.NewClient(opts...)
	stream := client.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(c.cfg.AnthropicModel),
		MaxTokens: 2048,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	})
	var all strings.Builder
	for stream.Next() {
		switch ev := stream.Current().AsAny().(type) {
		case anthropic.ContentBlockDeltaEvent:
			if d, ok := ev.Delta.AsAny().(anthropic.TextDelta); ok && d.Text != "" {
				all.WriteString(d.Text)
				out(d.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		var apiErr *anthropic.Error
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case 401:
				return all.String(), errors.New("Anthropic rejected the API key. Check it under Settings > AI")
			case 404:
				return all.String(), fmt.Errorf("Anthropic doesn't know the model %q", c.cfg.AnthropicModel)
			case 429:
				return all.String(), errors.New("Anthropic rate limit hit. Wait a moment and try again")
			}
			return all.String(), fmt.Errorf("Anthropic: %s", apiErr.Error())
		}
		return all.String(), err
	}
	return all.String(), nil
}
