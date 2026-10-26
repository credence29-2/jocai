package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestMCP(t *testing.T) {
	hs := newHarness(t)
	ctx := context.Background()
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := hs.a.MCPServer().Connect(ctx, t1, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	call := func(name string, args map[string]any) (string, map[string]any) {
		t.Helper()
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text += tc.Text
			}
		}
		if res.IsError {
			t.Fatalf("%s failed: %s", name, text)
		}
		var structured map[string]any
		if res.StructuredContent != nil {
			b, _ := json.Marshal(res.StructuredContent)
			json.Unmarshal(b, &structured)
		}
		return text, structured
	}

	var names []string
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "draft_mail,list_accounts,list_mail,read_mail" {
		t.Fatalf("tools %v", names)
	}

	text, _ := call("list_accounts", nil)
	if !strings.Contains(text, "sends as") || !strings.Contains(text, "support@credence.example") {
		t.Fatalf("accounts: %s", text)
	}

	text, out := call("list_mail", map[string]any{"query": "subject:\"Terminal 3\""})
	if !strings.Contains(text, "Terminal 3") || !strings.Contains(text, "[3 messages]") {
		t.Fatalf("list: %s", text)
	}
	rows := out["messages"].([]any)
	row := rows[0].(map[string]any)
	acct, id, thread := row["accountId"].(string), row["id"].(string), row["threadId"].(string)

	text, _ = call("read_mail", map[string]any{"accountId": acct, "threadId": thread})
	if strings.Count(text, "From: ") != 3 || !strings.Contains(text, "0.0.0.0") {
		t.Fatalf("read thread: %s", text)
	}
	text, _ = call("read_mail", map[string]any{"accountId": acct, "id": id})
	if strings.Count(text, "From: ") != 1 {
		t.Fatalf("read one: %s", text)
	}

	// A reply draft lands in the Drafts folder with the right threading,
	// recipients and signature.
	text, out = call("draft_mail", map[string]any{"from": "support@credence.example", "body": "Hi Maria,\n\nI'll call you in 15 minutes.", "replyAccountId": acct, "replyToId": id})
	if !strings.Contains(text, "Draft saved") || out["draftId"] == "" {
		t.Fatalf("draft: %s %v", text, out)
	}
	p, _, _ := hs.a.provider(acct)
	page, err := p.List(ctx, mail.Drafts, "", "", 10)
	if err != nil || len(page.Messages) != 1 {
		t.Fatalf("drafts: %v %v", page.Messages, err)
	}
	m, _ := p.Get(ctx, page.Messages[0].ID)
	if m.Subject != "Re: Terminal 3 won't print kitchen tickets" || len(m.To) != 1 || m.To[0].Email != "maria@tacoshack.example" ||
		!strings.Contains(m.HTML, "<div>Hi Maria,</div><div><br></div><div>I&#39;ll call you in 15 minutes.</div>") || !strings.Contains(m.HTML, "cm-signature") || !strings.Contains(m.HTML, "cm-quote") || m.ThreadID != thread {
		t.Fatalf("draft message: %+v\n%s", m, m.HTML)
	}

	// Errors come back as tool errors, not transport failures.
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "draft_mail", Arguments: map[string]any{"from": "nobody@x.example", "body": "x"}})
	if err != nil || !res.IsError || !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "list_accounts") {
		t.Fatalf("bad from: %+v %v", res, err)
	}
	res, _ = cs.CallTool(ctx, &mcp.CallToolParams{Name: "list_mail", Arguments: map[string]any{"folder": "junk"}})
	if !res.IsError {
		t.Fatal("unknown folder accepted")
	}
}
