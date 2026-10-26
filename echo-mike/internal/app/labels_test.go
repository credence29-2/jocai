package app

import (
	"testing"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

func TestLabels(t *testing.T) {
	hs := newHarness(t)
	var out labelsOut
	if code := hs.call("GET", "/api/labels", nil, &out); code != 200 || len(out.Labels) != 5 {
		t.Fatalf("labels %d %+v", code, out)
	}
	// Listing a label shows only messages carrying it; the list rows say
	// which labels each message has.
	var list listOut
	hs.call("POST", "/api/list", listIn{Folder: mail.LabelFolder("Label_1"), AccountID: "demo-g", Group: true}, &list)
	if len(list.Messages) != 2 {
		t.Fatalf("Clients label: %d rows", len(list.Messages))
	}
	var derek mail.Summary
	for _, m := range list.Messages {
		if m.From.Name == "Derek Chan" {
			derek = m
		}
	}
	if len(derek.Labels) != 2 {
		t.Fatalf("derek labels %v", derek.Labels)
	}
	// Remove Billing from Derek's message, add Schedule to Maria's thread.
	if code := hs.call("POST", "/api/label", map[string]any{"accountId": "demo-g", "ids": []string{derek.ID}, "labelId": "Label_2", "on": false}, nil); code != 200 {
		t.Fatal(code)
	}
	hs.call("POST", "/api/label", map[string]any{"accountId": "demo-g", "threads": []string{"1"}, "labelId": "Label_3", "on": true}, nil)
	hs.call("POST", "/api/list", listIn{Folder: mail.LabelFolder("Label_2"), AccountID: "demo-g"}, &list)
	if len(list.Messages) != 0 {
		t.Fatalf("Billing still has %d", len(list.Messages))
	}
	hs.call("POST", "/api/list", listIn{Folder: mail.LabelFolder("Label_3"), AccountID: "demo-g"}, &list)
	if len(list.Messages) != 4 { // Ana's archived mail plus the three in Maria's thread
		t.Fatalf("Schedule has %d", len(list.Messages))
	}
	// The Outlook-style box moves into the folder.
	hs.call("POST", "/api/label", map[string]any{"accountId": "demo-o", "ids": []string{"1"}, "labelId": "f1", "on": true}, nil)
	hs.call("POST", "/api/list", listIn{Folder: mail.LabelFolder("f1"), AccountID: "demo-o"}, &list)
	if len(list.Messages) != 1 {
		t.Fatalf("Dealers folder has %d", len(list.Messages))
	}
	hs.call("POST", "/api/list", listIn{Folder: mail.Inbox, AccountID: "demo-o"}, &list)
	for _, m := range list.Messages {
		if m.ID == "1" {
			t.Fatal("moved message still in inbox")
		}
	}
}
