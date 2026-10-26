package demo

import (
	"context"
	"fmt"

	"github.com/credence29-2/jocai/echo-mike/internal/mail"
)

var demoLabels = []mail.Label{{ID: "Label_1", Name: "Clients", Color: "#16a765"}, {ID: "Label_2", Name: "Billing", Color: "#ffad46"}, {ID: "Label_3", Name: "Schedule", Color: "#4986e7"}}

func (b *Box) Labels(context.Context) ([]mail.Label, error) {
	if b.aliases == nil || len(b.aliases) < 2 {
		return []mail.Label{{ID: "f1", Name: "Dealers"}, {ID: "f2", Name: "Reports"}}, nil
	}
	return demoLabels, nil
}

func (b *Box) SetLabel(_ context.Context, id, labelID string, on bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m, err := b.find(id)
	if err != nil {
		return err
	}
	var keep []string
	for _, l := range m.m.Labels {
		if l != labelID {
			keep = append(keep, l)
		}
	}
	if on {
		keep = append(keep, labelID)
	}
	m.m.Labels = keep
	if len(b.aliases) < 2 { // the Outlook-style box moves between folders
		if on {
			m.folder = mail.LabelFolder(labelID)
		} else {
			m.folder = mail.Inbox
		}
	}
	if m.m.Subject == "" {
		return fmt.Errorf("no such message")
	}
	return nil
}
