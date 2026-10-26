package gmail

import (
	"context"
	"net/url"
	"sort"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
)

// Labels lists the user's own labels, newest list cached for a few minutes.
func (c *Client) Labels(ctx context.Context) ([]cm.Label, error) {
	c.mu.Lock()
	if time.Since(c.labelsAt) < 5*time.Minute && c.labels != nil {
		out := c.labels
		c.mu.Unlock()
		return out, nil
	}
	c.mu.Unlock()
	var r struct {
		Labels []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Type  string `json:"type"`
			Color *struct {
				BackgroundColor string `json:"backgroundColor"`
			} `json:"color"`
		} `json:"labels"`
	}
	if err := c.do(ctx, "GET", "/gmail/v1/users/me/labels", nil, &r); err != nil {
		return nil, err
	}
	out := []cm.Label{}
	for _, l := range r.Labels {
		if l.Type != "user" {
			continue
		}
		lb := cm.Label{ID: l.ID, Name: l.Name}
		if l.Color != nil {
			lb.Color = l.Color.BackgroundColor
		}
		out = append(out, lb)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	c.mu.Lock()
	c.labels, c.labelsAt = out, time.Now()
	c.mu.Unlock()
	return out, nil
}

// SetLabel adds or removes one label on a message.
func (c *Client) SetLabel(ctx context.Context, id, labelID string, on bool) error {
	body := map[string][]string{}
	if on {
		body["addLabelIds"] = []string{labelID}
	} else {
		body["removeLabelIds"] = []string{labelID}
	}
	err := c.do(ctx, "POST", "/gmail/v1/users/me/messages/"+url.PathEscape(id)+"/modify", body, nil)
	if err == nil {
		c.touch(id, func(s *cm.Summary) {
			var keep []string
			for _, l := range s.Labels {
				if l != labelID {
					keep = append(keep, l)
				}
			}
			if on {
				keep = append(keep, labelID)
			}
			s.Labels = keep
		})
	}
	return err
}
