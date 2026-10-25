// Package api serves Victor Charlie's HTTP API, the realtime gateway and the web app.
package api

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/victorcharlie/internal/config"
	"github.com/credence29-2/jocai/victorcharlie/internal/media"
	"github.com/credence29-2/jocai/victorcharlie/internal/store"
)

type Server struct {
	cfg   *config.Config
	st    *store.Store
	media *media.Server
	hub   *Hub
	web   fs.FS

	limiter limiter
	slowMu  sync.Mutex
	slow    map[[2]store.ID]time.Time
}

func New(cfg *config.Config, st *store.Store, m *media.Server, web fs.FS) *Server {
	s := &Server{cfg: cfg, st: st, media: m, web: web, slow: map[[2]store.ID]time.Time{}}
	s.hub = newHub(s)
	go func() {
		for range time.Tick(30 * time.Minute) {
			s.limiter.sweep()
		}
	}()
	return s
}

const cookieName = "vc_session"

type ctxUser struct{}

// Handler is everything served on the public port.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	a := func(pattern string, h func(w http.ResponseWriter, r *http.Request, u *store.User)) {
		mux.HandleFunc(pattern, s.authed(h))
	}

	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("POST /api/auth/register", s.register)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("GET /api/invites/{code}", s.getInvite)
	mux.HandleFunc("GET /api/ws", s.gateway)

	a("GET /api/users/@me", s.me)
	a("PATCH /api/users/@me", s.patchMe)
	a("POST /api/users/@me/password", s.changePassword)
	a("POST /api/users/@me/avatar", s.uploadAvatar)
	a("POST /api/users/@me/channels", s.openDM)
	a("GET /api/users/{id}", s.getUser)

	a("GET /api/admin/users", s.adminUsers)
	a("PATCH /api/admin/users/{id}", s.adminPatchUser)
	a("GET /api/admin/settings", s.adminSettings)
	a("PATCH /api/admin/settings", s.adminPatchSettings)

	a("POST /api/invites/{code}", s.acceptInvite)
	a("POST /api/guilds", s.createGuild)
	a("PATCH /api/guilds/{gid}", s.patchGuild)
	a("DELETE /api/guilds/{gid}", s.deleteGuild)
	a("POST /api/guilds/{gid}/icon", s.uploadGuildIcon)
	a("DELETE /api/guilds/{gid}/members/@me", s.leaveGuild)
	a("PATCH /api/guilds/{gid}/members/{uid}", s.patchMember)
	a("DELETE /api/guilds/{gid}/members/{uid}", s.kickMember)
	a("GET /api/guilds/{gid}/bans", s.listBans)
	a("PUT /api/guilds/{gid}/bans/{uid}", s.banMember)
	a("DELETE /api/guilds/{gid}/bans/{uid}", s.unbanMember)
	a("POST /api/guilds/{gid}/roles", s.createRole)
	a("PATCH /api/guilds/{gid}/roles", s.reorderRoles)
	a("PATCH /api/guilds/{gid}/roles/{rid}", s.patchRole)
	a("DELETE /api/guilds/{gid}/roles/{rid}", s.deleteRole)
	a("POST /api/guilds/{gid}/channels", s.createChannel)
	a("PATCH /api/guilds/{gid}/channels", s.reorderChannels)
	a("GET /api/guilds/{gid}/invites", s.listInvites)
	a("POST /api/guilds/{gid}/invites", s.createInvite)
	a("DELETE /api/guilds/{gid}/invites/{code}", s.deleteInvite)

	a("PATCH /api/channels/{cid}", s.patchChannel)
	a("DELETE /api/channels/{cid}", s.deleteChannel)
	a("GET /api/channels/{cid}/messages", s.listMessages)
	a("POST /api/channels/{cid}/messages", s.postMessage)
	a("PATCH /api/channels/{cid}/messages/{mid}", s.editMessage)
	a("DELETE /api/channels/{cid}/messages/{mid}", s.deleteMessage)
	a("PUT /api/channels/{cid}/messages/{mid}/reactions/{emoji}", s.addReaction)
	a("DELETE /api/channels/{cid}/messages/{mid}/reactions/{emoji}", s.removeReaction)
	a("GET /api/channels/{cid}/pins", s.listPins)
	a("PUT /api/channels/{cid}/pins/{mid}", s.pin)
	a("DELETE /api/channels/{cid}/pins/{mid}", s.unpin)
	a("POST /api/channels/{cid}/typing", s.typing)
	a("POST /api/channels/{cid}/ack", s.ack)
	a("GET /api/search", s.search)

	a("POST /api/voice/{cid}/join", s.voiceJoin)
	a("POST /api/voice/{cid}/ring", s.voiceRing)

	a("GET /uploads/{id}/{name}", s.serveUpload)

	// LiveKit's signalling, so media setup rides the same HTTPS port.
	lk, _ := url.Parse("http://127.0.0.1:" + itoa(media.SignalPort))
	proxy := httputil.NewSingleHostReverseProxy(lk)
	proxy.ErrorLog = log.New(discard{}, "", 0)
	mux.Handle("/rtc", proxy)
	mux.Handle("/rtc/", proxy)

	mux.Handle("/", s.spa())
	return s.guard(mux)
}

// InternalHandler takes LiveKit's webhooks on a loopback-only port.
func (s *Server) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/livekit", s.livekitWebhook)
	return mux
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// guard adds security headers and blocks cross-site writes. Browsers can't
// send a custom header to another origin without a CORS preflight, which
// Victor Charlie never approves, so requiring one stops CSRF.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if !strings.HasPrefix(r.URL.Path, "/uploads/") {
			// Scripts only from this server: an injected <script> or inline
			// handler can't run even if some text slips through unescaped.
			h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' wss: ws:; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-VC") != "1" {
			httpError(w, http.StatusForbidden, "missing X-VC header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := s.web.Open(p); err == nil {
				f.Close()
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(p, "api/") {
			httpError(w, http.StatusNotFound, "no such endpoint")
			return
		}
		// Every other path is a route inside the app.
		w.Header().Set("Cache-Control", "no-cache")
		b, err := fs.ReadFile(s.web, "index.html")
		if err != nil {
			http.Error(w, "web app not built", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	})
}

func (s *Server) authed(h func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.st.SessionUser(sessionToken(r))
		if u == nil {
			httpError(w, http.StatusUnauthorized, "log in first")
			return
		}
		h(w, r, u)
	}
}

func sessionToken(r *http.Request) string {
	if c, err := r.Cookie(cookieName); err == nil {
		return c.Value
	}
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return a[7:]
	}
	return ""
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// fail maps store errors to status codes.
func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	case errors.Is(err, store.ErrTaken), errors.Is(err, store.ErrBanned):
		httpError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("error: %v", err)
		httpError(w, http.StatusInternalServerError, "something went wrong on the server")
	}
}

func forbidden(w http.ResponseWriter) {
	httpError(w, http.StatusForbidden, "you don't have permission to do that")
}

func badRequest(w http.ResponseWriter, msg string) {
	httpError(w, http.StatusBadRequest, msg)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		badRequest(w, "bad request body")
		return false
	}
	return true
}

func pathID(r *http.Request, name string) store.ID {
	id, _ := store.ParseID(r.PathValue(name))
	return id
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// limiter slows down password guessing: a few tries per minute per address.
type limiter struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	bytes map[string][]spent
}

// spend records n bytes against key and reports whether the total within
// per stays under max.
func (l *limiter) spend(key string, n, max int64, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bytes == nil {
		l.bytes = map[string][]spent{}
	}
	cut := time.Now().Add(-per)
	h := l.bytes[key]
	for len(h) > 0 && h[0].at.Before(cut) {
		h = h[1:]
	}
	var total int64
	for _, x := range h {
		total += x.n
	}
	if total+n > max {
		l.bytes[key] = h
		return false
	}
	l.bytes[key] = append(h, spent{time.Now(), n})
	return true
}

type spent struct {
	at time.Time
	n  int64
}

// sweep forgets keys with nothing recent, so the maps don't grow forever.
func (l *limiter) sweep() {
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := time.Now().Add(-2 * time.Hour)
	for k, h := range l.hits {
		if len(h) == 0 || h[len(h)-1].Before(cut) {
			delete(l.hits, k)
		}
	}
	for k, h := range l.bytes {
		if len(h) == 0 || h[len(h)-1].at.Before(cut) {
			delete(l.bytes, k)
		}
	}
}

func (l *limiter) allow(key string, n int, per time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	cut := time.Now().Add(-per)
	h := l.hits[key]
	for len(h) > 0 && h[0].Before(cut) {
		h = h[1:]
	}
	if len(h) >= n {
		l.hits[key] = h
		return false
	}
	l.hits[key] = append(h, time.Now())
	return true
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.HTTPS == "proxy" {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			return strings.TrimSpace(strings.Split(f, ",")[0])
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}
