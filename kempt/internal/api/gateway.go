package api

import (
	"encoding/json"
	"log"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/credence29-2/jocai/kempt/internal/store"
	"github.com/gorilla/websocket"
)

// Hub tracks who is connected, their presence, and voice states, and fans
// events out to the people allowed to see them.
type Hub struct {
	s     *Server
	mu    sync.Mutex
	conns map[store.ID]map[*conn]bool
	voice map[store.ID]*VoiceState // by user
	// pending holds the mute/deafen a client asked for when it requested a
	// voice token, applied once LiveKit says it joined.
	pending map[store.ID]VoiceState
	typing  map[[2]store.ID]time.Time
}

type VoiceState struct {
	UserID    store.ID `json:"user_id"`
	GuildID   store.ID `json:"guild_id"`
	ChannelID store.ID `json:"channel_id"`
	SelfMute  bool     `json:"self_mute"`
	SelfDeaf  bool     `json:"self_deaf"`
	Mute      bool     `json:"mute"`
	Deaf      bool     `json:"deaf"`
	Streaming bool     `json:"streaming"`
	Video     bool     `json:"video"`
	sid       string
}

type conn struct {
	ws   *websocket.Conn
	uid  store.ID
	send chan []byte
}

type event struct {
	T string `json:"t"`
	D any    `json:"d"`
}

func newHub(s *Server) *Hub {
	return &Hub{s: s, conns: map[store.ID]map[*conn]bool{}, voice: map[store.ID]*VoiceState{}, pending: map[store.ID]VoiceState{}, typing: map[[2]store.ID]time.Time{}}
}

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 16384}

func (s *Server) gateway(w http.ResponseWriter, r *http.Request) {
	u := s.st.SessionUser(sessionToken(r))
	if u == nil {
		httpError(w, http.StatusUnauthorized, "log in first")
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &conn{ws: ws, uid: u.ID, send: make(chan []byte, 256)}
	ready := s.readyPayload(u.ID)
	var b []byte
	s.st.Read(func() { b = encode("READY", ready) })
	c.send <- b

	h := s.hub
	h.mu.Lock()
	first := len(h.conns[u.ID]) == 0
	if h.conns[u.ID] == nil {
		h.conns[u.ID] = map[*conn]bool{}
	}
	h.conns[u.ID][c] = true
	h.mu.Unlock()
	if first {
		s.broadcastPresence(u.ID)
	}

	go c.writer()
	c.reader(s)

	h.mu.Lock()
	delete(h.conns[u.ID], c)
	last := len(h.conns[u.ID]) == 0
	if last {
		delete(h.conns, u.ID)
	}
	h.mu.Unlock()
	close(c.send)
	if last {
		s.broadcastPresence(u.ID)
	}
}

func (c *conn) writer() {
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	defer c.ws.Close()
	for {
		select {
		case b, ok := <-c.send:
			if !ok {
				c.ws.WriteControl(websocket.CloseMessage, nil, time.Now().Add(time.Second))
				return
			}
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		case <-ping.C:
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

type clientMsg struct {
	Op        string   `json:"op"`
	ChannelID store.ID `json:"channel_id"`
	SelfMute  *bool    `json:"self_mute"`
	SelfDeaf  *bool    `json:"self_deaf"`
	Streaming *bool    `json:"streaming"`
	Video     *bool    `json:"video"`
	Status    string   `json:"status"`
	Speaking  bool     `json:"speaking"`
}

func (c *conn) reader(s *Server) {
	c.ws.SetReadLimit(64 << 10)
	c.ws.SetReadDeadline(time.Now().Add(70 * time.Second))
	c.ws.SetPongHandler(func(string) error {
		c.ws.SetReadDeadline(time.Now().Add(70 * time.Second))
		return nil
	})
	for {
		_, b, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		c.ws.SetReadDeadline(time.Now().Add(70 * time.Second))
		var m clientMsg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		switch m.Op {
		case "ping":
			select {
			case c.send <- []byte(`{"t":"PONG","d":null}`):
			default:
			}
		case "voice_state":
			s.hub.updateSelfVoice(c.uid, m)
		}
	}
}

// --- fan-out ---

// toUsers sends an event to every connection of the given users.
func (h *Hub) toUsers(uids []store.ID, t string, d any) {
	h.toUsersRaw(uids, encode(t, d))
}

// encode marshals an event. Payloads that point into the store's cache must
// be encoded inside st.Read.
func encode(t string, d any) []byte {
	b, err := json.Marshal(event{t, d})
	if err != nil {
		log.Printf("marshal %s: %v", t, err)
	}
	return b
}

func (h *Hub) toUsersRaw(uids []store.ID, b []byte) {
	if b == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, uid := range uids {
		for c := range h.conns[uid] {
			select {
			case c.send <- b:
			default:
				// A client this far behind is gone; drop it so it reconnects
				// and gets a fresh READY.
				go c.ws.Close()
			}
		}
	}
}

func (h *Hub) online(uid store.ID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[uid]) > 0
}

// viewers lists who can see a channel. Call inside st.Read.
func (s *Server) viewers(c *store.Channel) []store.ID {
	if c.Type == "dm" {
		return slices.Clone(c.Recipients)
	}
	g := s.st.Guild(c.GuildID)
	if g == nil {
		return nil
	}
	var out []store.ID
	for uid := range g.Members {
		if g.In(uid, c)&store.PermViewChannel != 0 {
			out = append(out, uid)
		}
	}
	return out
}

func (s *Server) toChannel(cid store.ID, t string, d any) {
	var uids []store.ID
	var b []byte
	s.st.Read(func() {
		if c := s.st.Channel(cid); c != nil {
			uids = s.viewers(c)
		}
		b = encode(t, d)
	})
	s.hub.toUsersRaw(uids, b)
}

func (s *Server) toGuild(gid store.ID, t string, d any) {
	var uids []store.ID
	var b []byte
	s.st.Read(func() {
		if g := s.st.Guild(gid); g != nil {
			for uid := range g.Members {
				uids = append(uids, uid)
			}
		}
		b = encode(t, d)
	})
	s.hub.toUsersRaw(uids, b)
}

// syncGuild sends each member a fresh copy of the server as they can see
// it. Kempt does this after any change to roles, channels or members rather
// than working out which small event each person needs; servers are small
// and it keeps every client's view exactly right.
func (s *Server) syncGuild(gid store.ID) {
	type out struct {
		uid store.ID
		b   []byte
	}
	var all []out
	s.st.Read(func() {
		g := s.st.Guild(gid)
		if g == nil {
			return
		}
		for uid := range g.Members {
			all = append(all, out{uid, encode("GUILD_SYNC", s.guildPayload(g, uid))})
		}
	})
	for _, o := range all {
		s.hub.toUsersRaw([]store.ID{o.uid}, o.b)
	}
	s.refreshVoicePerms(gid)
}

// contacts is everyone who should hear about a user's presence or profile.
func (s *Server) contacts(uid store.ID) []store.ID {
	seen := map[store.ID]bool{uid: true}
	s.st.Read(func() {
		for _, g := range s.st.GuildsOf(uid) {
			for m := range g.Members {
				seen[m] = true
			}
		}
		for _, cid := range s.st.DMs(uid) {
			if c := s.st.Channel(cid); c != nil {
				for _, r := range c.Recipients {
					seen[r] = true
				}
			}
		}
	})
	out := make([]store.ID, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out
}

type Presence struct {
	UserID       store.ID `json:"user_id"`
	Status       string   `json:"status"`
	CustomStatus string   `json:"custom_status"`
}

func (s *Server) presenceOf(uid store.ID) Presence {
	p := Presence{UserID: uid, Status: "offline"}
	s.st.Read(func() {
		if u := s.st.User(uid); u != nil {
			p.CustomStatus = u.CustomStatus
			if s.hub.online(uid) && u.Status != "invisible" {
				p.Status = u.Status
			}
		}
	})
	return p
}

func (s *Server) broadcastPresence(uid store.ID) {
	s.hub.toUsers(s.contacts(uid), "PRESENCE", s.presenceOf(uid))
}

func (s *Server) broadcastUser(uid store.ID) {
	var u store.User
	s.st.Read(func() { u = *s.st.User(uid) })
	s.hub.toUsers(s.contacts(uid), "USER_UPDATE", u)
	s.broadcastPresence(uid)
}

// --- READY ---

type MemberPayload struct {
	*store.Member
	User *store.User `json:"user"`
}

type GuildPayload struct {
	*store.Guild
	Roles       []*store.Role    `json:"roles"`
	Channels    []*store.Channel `json:"channels"`
	Members     []MemberPayload  `json:"members"`
	Permissions uint64           `json:"permissions"`
}

// guildPayload is a server as uid sees it. Call inside st.Read.
func (s *Server) guildPayload(g *store.Guild, uid store.ID) *GuildPayload {
	p := &GuildPayload{Guild: g, Roles: g.SortedRoles(), Channels: []*store.Channel{}, Members: []MemberPayload{}, Permissions: g.Base(uid)}
	for _, c := range g.Channels {
		if g.In(uid, c)&store.PermViewChannel != 0 {
			p.Channels = append(p.Channels, c)
		}
	}
	slices.SortFunc(p.Channels, func(a, b *store.Channel) int { return a.Position - b.Position })
	for _, m := range g.Members {
		if u := s.st.User(m.UserID); u != nil {
			p.Members = append(p.Members, MemberPayload{m, u})
		}
	}
	return p
}

type DMPayload struct {
	*store.Channel
	Users []*store.User `json:"users"`
}

func (s *Server) dmPayload(c *store.Channel) DMPayload {
	p := DMPayload{Channel: c, Users: []*store.User{}}
	for _, r := range c.Recipients {
		if u := s.st.User(r); u != nil {
			p.Users = append(p.Users, u)
		}
	}
	return p
}

type Ready struct {
	User        *store.User       `json:"user"`
	Guilds      []*GuildPayload   `json:"guilds"`
	DMs         []DMPayload       `json:"dms"`
	ReadStates  []store.ReadState `json:"read_states"`
	VoiceStates []*VoiceState     `json:"voice_states"`
	Presences   []Presence        `json:"presences"`
	Status      string            `json:"status"`
	MaxUploadMB int               `json:"max_upload_mb"`
}

func (s *Server) readyPayload(uid store.ID) *Ready {
	r := &Ready{Guilds: []*GuildPayload{}, DMs: []DMPayload{}, MaxUploadMB: s.cfg.MaxUploadMB}
	visible := map[store.ID]bool{}
	roles := map[store.ID]bool{}
	people := map[store.ID]bool{}
	s.st.Read(func() {
		me := *s.st.User(uid)
		r.User = &me
		r.Status = me.Status
		for _, g := range s.st.GuildsOf(uid) {
			gp := s.guildPayload(g, uid)
			r.Guilds = append(r.Guilds, gp)
			for _, c := range gp.Channels {
				if c.Type == "text" {
					visible[c.ID] = true
				}
			}
			for _, rid := range g.Members[uid].Roles {
				roles[rid] = true
			}
			for m := range g.Members {
				people[m] = true
			}
		}
		for _, cid := range s.st.DMs(uid) {
			if c := s.st.Channel(cid); c != nil {
				r.DMs = append(r.DMs, s.dmPayload(c))
				visible[cid] = true
				for _, p := range c.Recipients {
					people[p] = true
				}
			}
		}
	})
	rs, err := s.st.ReadStates(uid, roles, visible)
	if err != nil {
		log.Printf("read states: %v", err)
	}
	r.ReadStates = rs
	if r.ReadStates == nil {
		r.ReadStates = []store.ReadState{}
	}
	r.VoiceStates = s.visibleVoice(uid)
	for p := range people {
		r.Presences = append(r.Presences, s.presenceOf(p))
	}
	return r
}
