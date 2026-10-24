package outlook

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	cm "github.com/credence29-2/jocai/echo-mike/internal/mail"
	"github.com/credence29-2/jocai/echo-mike/internal/oauth"
)

// SMTPConfig is where to send.
type SMTPConfig struct {
	Addr string
	// TLS overrides the TLS settings. Tests use it to trust a fake server.
	TLS *tls.Config
}

// SMTPDisabledError means the server refused SMTP sign-in, usually because
// a Microsoft 365 admin has SMTP AUTH turned off.
type SMTPDisabledError struct{ Msg string }

func (e *SMTPDisabledError) Error() string { return "SMTP sign-in refused: " + e.Msg }

// xoauth2 is SASL XOAUTH2, the OAuth sign-in for SMTP.
type xoauth2 struct{ user, token string }

func (a xoauth2) Start(*smtp.ServerInfo) (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + a.user + "\x01auth=Bearer " + a.token + "\x01\x01"), nil
}

func (a xoauth2) Next(_ []byte, more bool) ([]byte, error) {
	// On failure the server sends a JSON error as a challenge and expects an
	// empty reply before it returns the real error code.
	if more {
		return []byte{}, nil
	}
	return nil, nil
}

func (c *Client) sendSMTP(ctx context.Context, o *cm.Outgoing) error {
	raw, err := cm.Build(o, false, time.Now())
	if err != nil {
		return err
	}
	tok, err := c.Src.Token(ctx, oauth.SMTPScopes)
	if err != nil {
		return err
	}
	host, _, _ := net.SplitHostPort(c.SMTP.Addr)
	d := net.Dialer{Timeout: 20 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", c.SMTP.Addr)
	if err != nil {
		return fmt.Errorf("couldn't reach %s: %w", host, err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	conn.SetDeadline(deadline)
	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	tlsCfg := c.SMTP.TLS
	if tlsCfg == nil {
		tlsCfg = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	}
	if ok, _ := cl.Extension("STARTTLS"); !ok {
		return fmt.Errorf("%s doesn't offer an encrypted connection; not sending", host)
	}
	if err := cl.StartTLS(tlsCfg); err != nil {
		return err
	}
	if err := cl.Auth(xoauth2{user: c.Email, token: tok}); err != nil {
		var te *textproto.Error
		if errors.As(err, &te) && (te.Code == 535 || te.Code == 534 || te.Code == 504) {
			return &SMTPDisabledError{Msg: strings.TrimSpace(te.Msg)}
		}
		return err
	}
	if err := cl.Mail(o.From.Email); err != nil {
		return fmt.Errorf("server refused the From address %s: %w", o.From.Email, err)
	}
	for _, r := range cm.Recipients(o) {
		if err := cl.Rcpt(r); err != nil {
			return fmt.Errorf("server refused %s: %w", r, err)
		}
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		if strings.Contains(err.Error(), "SendAsDenied") {
			return fmt.Errorf("Microsoft won't let this account send as %s. Make sure it's an alias on the account (personal) or that your admin allows sending from aliases (work)", o.From.Email)
		}
		return err
	}
	return cl.Quit()
}
