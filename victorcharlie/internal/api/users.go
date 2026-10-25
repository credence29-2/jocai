package api

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/credence29-2/jocai/victorcharlie/internal/store"
)

var errNeedInvite = errors.New("invite required")

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.]{2,32}$`)

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"name":              s.cfg.AppName,
		"setup":             s.st.UserCount() == 0,
		"open_registration": s.cfg.OpenRegistration,
	})
}

func (s *Server) setSession(w http.ResponseWriter, r *http.Request, uid store.ID) (string, error) {
	tok, err := s.st.NewSession(uid, r.UserAgent())
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().AddDate(1, 0, 0),
	})
	return tok, nil
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Invite   string `json:"invite"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.limiter.allow("reg:"+s.clientIP(r), 5, time.Hour) {
		httpError(w, http.StatusTooManyRequests, "too many new accounts from here, try again later")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if !usernameRe.MatchString(body.Username) {
		badRequest(w, "usernames are 2 to 32 letters, numbers, dots or underscores")
		return
	}
	if len(body.Password) < 8 {
		badRequest(w, "use a password of at least 8 characters")
		return
	}
	var inv *store.Invite
	if body.Invite != "" {
		var err error
		if inv, err = s.st.Invite(body.Invite); err != nil {
			badRequest(w, "that invite link is expired or invalid")
			return
		}
	}
	u, first, err := s.st.CreateUser(body.Username, body.Password, func(first bool) error {
		if !first && !s.cfg.OpenRegistration && inv == nil {
			return errNeedInvite
		}
		return nil
	})
	if err == errNeedInvite {
		httpError(w, http.StatusForbidden, "you need an invite link to make an account here")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	if first {
		if _, err := s.st.CreateGuild(u.ID, "Home"); err != nil {
			fail(w, err)
			return
		}
	}
	if inv != nil {
		s.joinGuild(inv, u.ID)
	}
	if _, err := s.setSession(w, r, u.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if !s.limiter.allow("login:"+s.clientIP(r), 10, time.Minute) {
		httpError(w, http.StatusTooManyRequests, "too many tries, wait a minute")
		return
	}
	// Per account too, so guessing from many addresses doesn't help.
	if !s.limiter.allow("acct:"+strings.ToLower(strings.TrimSpace(body.Username)), 20, 15*time.Minute) {
		httpError(w, http.StatusTooManyRequests, "too many tries for that account, wait 15 minutes")
		return
	}
	u := s.st.CheckPassword(strings.TrimSpace(body.Username), body.Password)
	if u == nil {
		httpError(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	if _, err := s.setSession(w, r, u.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"user": u})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.st.DeleteSession(sessionToken(r))
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, u)
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := pathID(r, "id")
	var out *store.User
	s.st.Read(func() {
		if o := s.st.User(id); o != nil && (o.ID == u.ID || s.st.SharesGuild(u.ID, id) || u.IsAdmin) {
			cp := *o
			out = &cp
		}
	})
	if out == nil {
		fail(w, store.ErrNotFound)
		return
	}
	writeJSON(w, out)
}

func (s *Server) patchMe(w http.ResponseWriter, r *http.Request, u *store.User) {
	var p store.UserPatch
	if !decode(w, r, &p) {
		return
	}
	p.Avatar, p.Disabled = nil, nil
	if p.Username != nil && !usernameRe.MatchString(*p.Username) {
		badRequest(w, "usernames are 2 to 32 letters, numbers, dots or underscores")
		return
	}
	if p.DisplayName != nil {
		t := strings.TrimSpace(*p.DisplayName)
		if t == "" || utf8.RuneCountInString(t) > 32 {
			badRequest(w, "display names are 1 to 32 characters")
			return
		}
		p.DisplayName = &t
	}
	if p.About != nil && utf8.RuneCountInString(*p.About) > 190 {
		badRequest(w, "keep the about me under 190 characters")
		return
	}
	if p.CustomStatus != nil && utf8.RuneCountInString(*p.CustomStatus) > 64 {
		badRequest(w, "keep the status under 64 characters")
		return
	}
	if p.Status != nil {
		switch *p.Status {
		case "online", "idle", "dnd", "invisible":
		default:
			badRequest(w, "bad status")
			return
		}
	}
	nu, err := s.st.UpdateUser(u.ID, p)
	if err != nil {
		fail(w, err)
		return
	}
	s.broadcastUser(u.ID)
	s.st.Read(func() { writeJSON(w, nu) })
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !decode(w, r, &body) {
		return
	}
	if s.st.CheckPassword(u.Username, body.Current) == nil {
		httpError(w, http.StatusUnauthorized, "your current password is wrong")
		return
	}
	if len(body.New) < 8 {
		badRequest(w, "use a password of at least 8 characters")
		return
	}
	if err := s.st.SetPassword(u.ID, body.New); err != nil {
		fail(w, err)
		return
	}
	s.st.DeleteSessions(u.ID, sessionToken(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uploadAvatar(w http.ResponseWriter, r *http.Request, u *store.User) {
	a, ok := s.receiveImage(w, r, u.ID)
	if !ok {
		return
	}
	var old string
	s.st.Read(func() { old = s.st.User(u.ID).Avatar })
	if _, err := s.st.UpdateUser(u.ID, store.UserPatch{Avatar: &a.URL}); err != nil {
		s.removeAttachments([]store.Attachment{a})
		fail(w, err)
		return
	}
	s.removeAttachments([]store.Attachment{{URL: old}})
	s.broadcastUser(u.ID)
	writeJSON(w, map[string]string{"avatar": a.URL})
}

func (s *Server) openDM(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Recipients []store.ID `json:"recipients"`
	}
	if !decode(w, r, &body) {
		return
	}
	seen := map[store.ID]bool{u.ID: true}
	var others []store.ID
	okAll := true
	s.st.Read(func() {
		for _, id := range body.Recipients {
			if seen[id] {
				continue
			}
			seen[id] = true
			if s.st.User(id) == nil || !s.st.SharesGuild(u.ID, id) {
				okAll = false
			}
			others = append(others, id)
		}
	})
	if !okAll || len(others) == 0 || len(others) > 9 {
		badRequest(w, "you can message up to 9 people you share a server with")
		return
	}
	c, created, err := s.st.OpenDM(u.ID, others)
	if err != nil {
		fail(w, err)
		return
	}
	var b []byte
	var p DMPayload
	s.st.Read(func() {
		p = s.dmPayload(c)
		b = encode("DM_CREATE", p)
	})
	if created {
		s.hub.toUsersRaw(append(others, u.ID), b)
	}
	s.st.Read(func() { writeJSON(w, p) })
}

// --- server admin (whoever made the first account) ---

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !u.IsAdmin {
		forbidden(w)
		return
	}
	type row struct {
		store.User
		Disabled bool `json:"disabled"`
		Online   bool `json:"online"`
	}
	var out []row
	s.st.Read(func() {
		for _, o := range s.st.Users() {
			out = append(out, row{*o, o.Disabled, s.hub.online(o.ID)})
		}
	})
	writeJSON(w, out)
}

func (s *Server) adminPatchUser(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !u.IsAdmin {
		forbidden(w)
		return
	}
	id := pathID(r, "id")
	var body struct {
		Disabled *bool  `json:"disabled"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	if id == u.ID && body.Disabled != nil && *body.Disabled {
		badRequest(w, "you can't disable your own account")
		return
	}
	if body.Password != "" {
		if len(body.Password) < 8 {
			badRequest(w, "use a password of at least 8 characters")
			return
		}
		if err := s.st.SetPassword(id, body.Password); err != nil {
			fail(w, err)
			return
		}
		s.st.DeleteSessions(id, "")
	}
	if body.Disabled != nil {
		if _, err := s.st.UpdateUser(id, store.UserPatch{Disabled: body.Disabled}); err != nil {
			fail(w, err)
			return
		}
		if *body.Disabled {
			s.st.DeleteSessions(id, "")
			s.disconnectVoice(id)
			s.hub.closeUser(id)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !u.IsAdmin {
		forbidden(w)
		return
	}
	writeJSON(w, map[string]any{"open_registration": s.cfg.OpenRegistration, "max_upload_mb": s.cfg.MaxUploadMB})
}

func (s *Server) adminPatchSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !u.IsAdmin {
		forbidden(w)
		return
	}
	var body struct {
		OpenRegistration *bool `json:"open_registration"`
		MaxUploadMB      *int  `json:"max_upload_mb"`
	}
	if !decode(w, r, &body) {
		return
	}
	set(&s.cfg.OpenRegistration, body.OpenRegistration)
	if body.MaxUploadMB != nil && *body.MaxUploadMB >= 1 && *body.MaxUploadMB <= 2048 {
		s.cfg.MaxUploadMB = *body.MaxUploadMB
	}
	if err := s.cfg.Save(); err != nil {
		fail(w, err)
		return
	}
	s.adminSettings(w, r, u)
}

func (h *Hub) closeUser(uid store.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns[uid] {
		go c.ws.Close()
	}
}
