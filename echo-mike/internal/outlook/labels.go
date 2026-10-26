package outlook

import (
	"context"
	"net/url"
	"sort"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// standardFolders are Outlook's own folders, left out of the label list.
var standardFolders = map[string]bool{
	"Inbox": true, "Drafts": true, "Sent Items": true, "Deleted Items": true, "Junk Email": true, "Archive": true, "Outbox": true,
	"Conversation History": true, "Sync Issues": true, "RSS Feeds": true, "Notes": true, "Journal": true, "Clutter": true, "Scheduled": true, "Snoozed": true,
}

// Labels lists the mailbox's own folders. Outlook has no labels; moving
// a message into a folder is the closest thing.
func (c *Client) Labels(ctx context.Context) ([]cm.Label, error) {
	c.mu.Lock()
	if time.Since(c.labelsAt) < 5*time.Minute && c.labels != nil {
		out := c.labels
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()
	var r struct {
		Value []struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"value"`
	}
	if err := c.do(ctx, "GET", c.Base+"/me/mailFolders?$top=200&$select=id,displayName", nil, &r); err != nil {
		return nil, err
	}
	out := []cm.Label{}
	for _, f := range r.Value {
		if standardFolders[f.DisplayName] {
			continue
		}
		out = append(out, cm.Label{ID: f.ID, Name: f.DisplayName})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.mu.Lock()
	c.labels, c.labelsAt = out, time.Now()
	c.mu.Unlock()
	return out, nil
}

// SetLabel moves the message into the folder, or back to the inbox.
func (c *Client) SetLabel(ctx context.Context, id, labelID string, on bool) error {
	dest := "inbox"
	if on {
		dest = labelID
	}
	return c.do(ctx, "POST", c.msgURL(id)+"/move", map[string]string{"destinationId": dest}, nil)
}

func folderMessages(base, id string) string {
	return base + "/me/mailFolders/" + url.PathEscape(id) + "/messages"
}
