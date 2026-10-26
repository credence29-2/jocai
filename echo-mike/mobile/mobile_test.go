package mobile

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeHost struct{ notes []string }

func (h *fakeHost) OpenURL(string)                      {}
func (h *fakeHost) ShowNotification(title, body string) { h.notes = append(h.notes, title) }
func (h *fakeHost) Refresh()                            {}

func TestStartServesThePage(t *testing.T) {
	h := &fakeHost{}
	u, err := Start(t.TempDir(), true, h)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Start("ignored", true, h)
	if again != u {
		t.Fatal("second Start made another server")
	}
	resp, err := http.Get(strings.SplitN(u, "#", 2)[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), "<title>Project Echo Mike</title>") {
		t.Fatal("page not served")
	}
	if err := CheckMail("ignored", h); err != nil {
		t.Fatal(err)
	}
}
