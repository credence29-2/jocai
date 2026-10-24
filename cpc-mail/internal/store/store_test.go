package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/credence29-2/jocai/cpc-mail/internal/mail"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Update(func(d *Data) error {
		d.Accounts = append(d.Accounts, Account{ID: "a1", Provider: "gmail", Email: "j@gmail.com"})
		d.Identities = append(d.Identities, mail.Identity{ID: "i1", AccountID: "a1", Email: "j@gmail.com", SignatureHTML: "<b>J</b>"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetSecret("a1", "refresh-xyz"); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	got.Accounts[0].Email = "changed"
	if s.Get().Accounts[0].Email != "j@gmail.com" {
		t.Fatal("Get must return a copy")
	}

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := s2.Get()
	if len(d.Identities) != 1 || d.Identities[0].SignatureHTML != "<b>J</b>" || s2.Secret("a1") != "refresh-xyz" {
		t.Fatalf("reloaded = %+v", d)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "config.json")); len(b) == 0 || containsToken(b) {
		t.Fatal("token must not be in config.json")
	}
	s2.SetSecret("a1", "")
	s3, _ := Open(dir)
	if s3.Secret("a1") != "" {
		t.Fatal("secret not removed")
	}
}

func containsToken(b []byte) bool {
	return strings.Contains(string(b), "refresh-xyz")
}
