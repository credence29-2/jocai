package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           ID     `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	Avatar       string `json:"avatar"`
	About        string `json:"about"`
	Accent       int    `json:"accent"`
	Status       string `json:"-"` // chosen status; online/idle/dnd/invisible
	CustomStatus string `json:"custom_status"`
	IsAdmin      bool   `json:"is_admin,omitempty"`
	Disabled     bool   `json:"-"`
	passwordHash string
}

type Role struct {
	ID          ID     `json:"id"`
	GuildID     ID     `json:"guild_id"`
	Name        string `json:"name"`
	Color       int    `json:"color"`
	Position    int    `json:"position"`
	Permissions uint64 `json:"permissions"`
	Hoist       bool   `json:"hoist"`
	Mentionable bool   `json:"mentionable"`
}

type Overwrite struct {
	ID    ID     `json:"id"` // role id, guild id for @everyone, or user id
	Allow uint64 `json:"allow"`
	Deny  uint64 `json:"deny"`
}

type Channel struct {
	ID            ID          `json:"id"`
	GuildID       ID          `json:"guild_id"`
	Type          string      `json:"type"` // text, voice, category, dm
	Name          string      `json:"name"`
	Topic         string      `json:"topic"`
	Position      int         `json:"position"`
	ParentID      ID          `json:"parent_id"`
	Overwrites    []Overwrite `json:"overwrites"`
	Bitrate       int         `json:"bitrate"` // kbps, voice only
	UserLimit     int         `json:"user_limit"`
	Slowmode      int         `json:"slowmode"`
	OwnerID       ID          `json:"owner_id"` // group DMs
	Recipients    []ID        `json:"recipients,omitempty"`
	LastMessageID ID          `json:"last_message_id"`
}

type Member struct {
	GuildID  ID     `json:"guild_id"`
	UserID   ID     `json:"user_id"`
	Nick     string `json:"nick"`
	Roles    []ID   `json:"roles"`
	JoinedAt int64  `json:"joined_at"`
	Mute     bool   `json:"mute"`
	Deaf     bool   `json:"deaf"`
}

type Guild struct {
	ID              ID     `json:"id"`
	Name            string `json:"name"`
	Icon            string `json:"icon"`
	OwnerID         ID     `json:"owner_id"`
	AFKChannelID    ID     `json:"afk_channel_id"`
	SystemChannelID ID     `json:"system_channel_id"`

	Roles    map[ID]*Role    `json:"-"`
	Channels map[ID]*Channel `json:"-"`
	Members  map[ID]*Member  `json:"-"`
	Bans     map[ID]bool     `json:"-"`
}

func (s *Store) load() error {
	s.users = map[ID]*User{}
	s.byName = map[string]ID{}
	s.guilds = map[ID]*Guild{}
	s.channels = map[ID]*Channel{}
	s.dms = map[ID][]ID{}

	rows, err := s.db.Query(`SELECT id, username, display_name, password_hash, avatar, about, accent, status, custom_status, is_admin, disabled FROM users`)
	if err != nil {
		return err
	}
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.passwordHash, &u.Avatar, &u.About, &u.Accent, &u.Status, &u.CustomStatus, &u.IsAdmin, &u.Disabled); err != nil {
			return err
		}
		s.users[u.ID] = u
		s.byName[strings.ToLower(u.Username)] = u.ID
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id, name, icon, owner_id, afk_channel_id, system_channel_id FROM guilds`)
	if err != nil {
		return err
	}
	for rows.Next() {
		g := &Guild{Roles: map[ID]*Role{}, Channels: map[ID]*Channel{}, Members: map[ID]*Member{}, Bans: map[ID]bool{}}
		if err := rows.Scan(&g.ID, &g.Name, &g.Icon, &g.OwnerID, &g.AFKChannelID, &g.SystemChannelID); err != nil {
			return err
		}
		s.guilds[g.ID] = g
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id, guild_id, name, color, position, permissions, hoist, mentionable FROM roles`)
	if err != nil {
		return err
	}
	for rows.Next() {
		r := &Role{}
		if err := rows.Scan(&r.ID, &r.GuildID, &r.Name, &r.Color, &r.Position, &r.Permissions, &r.Hoist, &r.Mentionable); err != nil {
			return err
		}
		if g := s.guilds[r.GuildID]; g != nil {
			g.Roles[r.ID] = r
		}
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id, guild_id, type, name, topic, position, parent_id, overwrites, bitrate, user_limit, slowmode, owner_id, last_message_id FROM channels`)
	if err != nil {
		return err
	}
	for rows.Next() {
		c := &Channel{}
		var ow string
		if err := rows.Scan(&c.ID, &c.GuildID, &c.Type, &c.Name, &c.Topic, &c.Position, &c.ParentID, &ow, &c.Bitrate, &c.UserLimit, &c.Slowmode, &c.OwnerID, &c.LastMessageID); err != nil {
			return err
		}
		json.Unmarshal([]byte(ow), &c.Overwrites)
		s.channels[c.ID] = c
		if g := s.guilds[c.GuildID]; g != nil {
			g.Channels[c.ID] = c
		}
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT channel_id, user_id FROM dm_recipients ORDER BY rowid`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var cid, uid ID
		if err := rows.Scan(&cid, &uid); err != nil {
			return err
		}
		if c := s.channels[cid]; c != nil {
			c.Recipients = append(c.Recipients, uid)
			s.dms[uid] = append(s.dms[uid], cid)
		}
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT guild_id, user_id, nick, roles, joined_at, mute, deaf FROM members`)
	if err != nil {
		return err
	}
	for rows.Next() {
		m := &Member{}
		var roles string
		if err := rows.Scan(&m.GuildID, &m.UserID, &m.Nick, &roles, &m.JoinedAt, &m.Mute, &m.Deaf); err != nil {
			return err
		}
		json.Unmarshal([]byte(roles), &m.Roles)
		if m.Roles == nil {
			m.Roles = []ID{}
		}
		if g := s.guilds[m.GuildID]; g != nil {
			g.Members[m.UserID] = m
		}
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT guild_id, user_id FROM bans`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var gid, uid ID
		if err := rows.Scan(&gid, &uid); err != nil {
			return err
		}
		if g := s.guilds[gid]; g != nil {
			g.Bans[uid] = true
		}
	}
	return rows.Close()
}

// Read runs fn while holding the read lock, for code that walks the cached
// state (permission checks, building payloads).
func (s *Store) Read(fn func()) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	fn()
}

// The getters below must be called inside Read or a store method.

func (s *Store) User(id ID) *User       { return s.users[id] }
func (s *Store) Guild(id ID) *Guild     { return s.guilds[id] }
func (s *Store) Channel(id ID) *Channel { return s.channels[id] }
func (s *Store) DMs(uid ID) []ID        { return s.dms[uid] }

func (s *Store) UserByName(name string) *User {
	return s.users[s.byName[strings.ToLower(name)]]
}

func (s *Store) Users() []*User {
	out := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		out = append(out, u)
	}
	return out
}

// GuildsOf lists the servers a user belongs to, oldest first.
func (s *Store) GuildsOf(uid ID) []*Guild {
	var out []*Guild
	for _, g := range s.guilds {
		if g.Members[uid] != nil {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) UserCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users)
}

// SharesGuild reports whether two users have a server in common, which is
// what lets them DM each other.
func (s *Store) SharesGuild(a, b ID) bool {
	for _, g := range s.guilds {
		if g.Members[a] != nil && g.Members[b] != nil {
			return true
		}
	}
	return false
}

// --- users and sessions ---

var ErrTaken = errors.New("that username is taken")

// CreateUser makes an account. The very first account is the admin; allow
// decides, under the same lock, whether this signup may go ahead given
// whether it would be the first, so two signups at once can't both be.
func (s *Store) CreateUser(username, password string, allow func(first bool) error) (*User, bool, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 11)
	if err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	first := len(s.users) == 0
	if err := allow(first); err != nil {
		return nil, false, err
	}
	admin := first
	if _, ok := s.byName[strings.ToLower(username)]; ok {
		return nil, false, ErrTaken
	}
	u := &User{ID: NewID(), Username: username, DisplayName: username, Status: "online", IsAdmin: admin, passwordHash: string(hash)}
	if _, err := s.db.Exec(`INSERT INTO users (id, username, display_name, password_hash, is_admin) VALUES (?, ?, ?, ?, ?)`, u.ID, u.Username, u.DisplayName, u.passwordHash, b2i(admin)); err != nil {
		return nil, false, err
	}
	s.users[u.ID] = u
	s.byName[strings.ToLower(username)] = u.ID
	return u, first, nil
}

var (
	dummyOnce sync.Once
	dummyHash []byte
)

// CheckPassword returns the user if the password matches.
func (s *Store) CheckPassword(username, password string) *User {
	s.mu.RLock()
	u := s.UserByName(username)
	var hash string
	if u != nil {
		hash = u.passwordHash
	}
	s.mu.RUnlock()
	if u == nil {
		// Spend the same time as a real check so usernames can't be probed.
		dummyOnce.Do(func() { dummyHash, _ = bcrypt.GenerateFromPassword([]byte("x"), 11) })
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil || u.Disabled {
		return nil
	}
	return u
}

func (s *Store) SetPassword(uid ID, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 11)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, string(hash), uid); err != nil {
		return err
	}
	s.users[uid].passwordHash = string(hash)
	return nil
}

type UserPatch struct {
	Username     *string `json:"username"`
	DisplayName  *string `json:"display_name"`
	Avatar       *string `json:"avatar"`
	About        *string `json:"about"`
	Accent       *int    `json:"accent"`
	Status       *string `json:"status"`
	CustomStatus *string `json:"custom_status"`
	Disabled     *bool   `json:"disabled"`
}

func (s *Store) UpdateUser(uid ID, p UserPatch) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.users[uid]
	if u == nil {
		return nil, ErrNotFound
	}
	nu := *u
	if p.Username != nil && !strings.EqualFold(*p.Username, u.Username) {
		if _, ok := s.byName[strings.ToLower(*p.Username)]; ok {
			return nil, ErrTaken
		}
	}
	set(&nu.Username, p.Username)
	set(&nu.DisplayName, p.DisplayName)
	set(&nu.Avatar, p.Avatar)
	set(&nu.About, p.About)
	set(&nu.Accent, p.Accent)
	set(&nu.Status, p.Status)
	set(&nu.CustomStatus, p.CustomStatus)
	set(&nu.Disabled, p.Disabled)
	if _, err := s.db.Exec(`UPDATE users SET username=?, display_name=?, avatar=?, about=?, accent=?, status=?, custom_status=?, disabled=? WHERE id=?`,
		nu.Username, nu.DisplayName, nu.Avatar, nu.About, nu.Accent, nu.Status, nu.CustomStatus, b2i(nu.Disabled), uid); err != nil {
		return nil, err
	}
	delete(s.byName, strings.ToLower(u.Username))
	*u = nu
	s.byName[strings.ToLower(u.Username)] = uid
	return u, nil
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func hashToken(tok string) string {
	h := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(h[:])
}

func RandomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Store) NewSession(uid ID, agent string) (string, error) {
	tok := RandomToken(32)
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, created_at, last_seen, agent) VALUES (?, ?, ?, ?, ?)`, hashToken(tok), uid, now(), now(), agent)
	return tok, err
}

// SessionUser resolves a session token to its user.
func (s *Store) SessionUser(tok string) *User {
	if tok == "" {
		return nil
	}
	var uid ID
	var last int64
	h := hashToken(tok)
	if err := s.db.QueryRow(`SELECT user_id, last_seen FROM sessions WHERE token_hash = ?`, h).Scan(&uid, &last); err != nil {
		return nil
	}
	if now()-last > 90*24*3600_000 {
		s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, h)
		return nil
	}
	if now()-last > 3600_000 {
		s.db.Exec(`UPDATE sessions SET last_seen = ? WHERE token_hash = ?`, now(), h)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.users[uid]
	if u == nil || u.Disabled {
		return nil
	}
	return u
}

func (s *Store) DeleteSession(tok string) {
	s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, hashToken(tok))
}

// DeleteSessions logs a user out everywhere except keep (may be empty).
func (s *Store) DeleteSessions(uid ID, keep string) {
	s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, uid, hashToken(keep))
}

// --- guilds ---

// CreateGuild makes a server with @everyone, a text channel and a voice
// channel, owned by uid.
func (s *Store) CreateGuild(uid ID, name string) (*Guild, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := &Guild{ID: NewID(), Name: name, OwnerID: uid, Roles: map[ID]*Role{}, Channels: map[ID]*Channel{}, Members: map[ID]*Member{}, Bans: map[ID]bool{}}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	everyone := &Role{ID: g.ID, GuildID: g.ID, Name: "@everyone", Permissions: PermDefault}
	textCat := &Channel{ID: NewID(), GuildID: g.ID, Type: "category", Name: "Text Channels", Overwrites: []Overwrite{}}
	voiceCat := &Channel{ID: NewID(), GuildID: g.ID, Type: "category", Name: "Voice Channels", Position: 1, Overwrites: []Overwrite{}}
	general := &Channel{ID: NewID(), GuildID: g.ID, Type: "text", Name: "general", ParentID: textCat.ID, Overwrites: []Overwrite{}}
	lounge := &Channel{ID: NewID(), GuildID: g.ID, Type: "voice", Name: "General", ParentID: voiceCat.ID, Bitrate: 96, Overwrites: []Overwrite{}}
	g.SystemChannelID = general.ID
	if _, err := tx.Exec(`INSERT INTO guilds (id, name, owner_id, system_channel_id) VALUES (?, ?, ?, ?)`, g.ID, g.Name, uid, g.SystemChannelID); err != nil {
		return nil, err
	}
	if err := insertRole(tx, everyone); err != nil {
		return nil, err
	}
	for _, c := range []*Channel{textCat, voiceCat, general, lounge} {
		if err := insertChannel(tx, c); err != nil {
			return nil, err
		}
	}
	m := &Member{GuildID: g.ID, UserID: uid, Roles: []ID{}, JoinedAt: now()}
	if _, err := tx.Exec(`INSERT INTO members (guild_id, user_id, joined_at) VALUES (?, ?, ?)`, g.ID, uid, m.JoinedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	g.Roles[everyone.ID] = everyone
	for _, c := range []*Channel{textCat, voiceCat, general, lounge} {
		g.Channels[c.ID] = c
		s.channels[c.ID] = c
	}
	g.Members[uid] = m
	s.guilds[g.ID] = g
	return g, nil
}

type GuildPatch struct {
	Name            *string `json:"name"`
	Icon            *string `json:"icon"`
	OwnerID         *ID     `json:"owner_id"`
	AFKChannelID    *ID     `json:"afk_channel_id"`
	SystemChannelID *ID     `json:"system_channel_id"`
}

func (s *Store) UpdateGuild(gid ID, p GuildPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return ErrNotFound
	}
	ng := *g
	set(&ng.Name, p.Name)
	set(&ng.Icon, p.Icon)
	set(&ng.OwnerID, p.OwnerID)
	set(&ng.AFKChannelID, p.AFKChannelID)
	set(&ng.SystemChannelID, p.SystemChannelID)
	if _, err := s.db.Exec(`UPDATE guilds SET name=?, icon=?, owner_id=?, afk_channel_id=?, system_channel_id=? WHERE id=?`,
		ng.Name, ng.Icon, ng.OwnerID, ng.AFKChannelID, ng.SystemChannelID, gid); err != nil {
		return err
	}
	*g = ng
	return nil
}

func (s *Store) DeleteGuild(gid ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for cid := range g.Channels {
		if _, err := tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, cid); err != nil {
			return err
		}
		tx.Exec(`DELETE FROM read_states WHERE channel_id = ?`, cid)
	}
	tx.Exec(`DELETE FROM channels WHERE guild_id = ?`, gid)
	if _, err := tx.Exec(`DELETE FROM guilds WHERE id = ?`, gid); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for cid := range g.Channels {
		delete(s.channels, cid)
	}
	delete(s.guilds, gid)
	return nil
}

// --- roles ---

func insertRole(e execer, r *Role) error {
	_, err := e.Exec(`INSERT INTO roles (id, guild_id, name, color, position, permissions, hoist, mentionable) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.GuildID, r.Name, r.Color, r.Position, r.Permissions, b2i(r.Hoist), b2i(r.Mentionable))
	return err
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

// CreateRole adds a role just above @everyone.
func (s *Store) CreateRole(gid ID, r Role) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return nil, ErrNotFound
	}
	r.ID = NewID()
	r.GuildID = gid
	r.Position = 1
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, o := range g.Roles {
		if o.ID != gid {
			if _, err := tx.Exec(`UPDATE roles SET position = position + 1 WHERE id = ?`, o.ID); err != nil {
				return nil, err
			}
		}
	}
	if err := insertRole(tx, &r); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	for _, o := range g.Roles {
		if o.ID != gid {
			o.Position++
		}
	}
	g.Roles[r.ID] = &r
	return &r, nil
}

type RolePatch struct {
	Name        *string `json:"name"`
	Color       *int    `json:"color"`
	Permissions *uint64 `json:"permissions"`
	Hoist       *bool   `json:"hoist"`
	Mentionable *bool   `json:"mentionable"`
}

func (s *Store) UpdateRole(gid, rid ID, p RolePatch) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil || g.Roles[rid] == nil {
		return nil, ErrNotFound
	}
	r := g.Roles[rid]
	nr := *r
	set(&nr.Name, p.Name)
	set(&nr.Color, p.Color)
	set(&nr.Permissions, p.Permissions)
	set(&nr.Hoist, p.Hoist)
	set(&nr.Mentionable, p.Mentionable)
	if rid == gid {
		nr.Name = "@everyone"
		nr.Hoist = false
	}
	if _, err := s.db.Exec(`UPDATE roles SET name=?, color=?, permissions=?, hoist=?, mentionable=? WHERE id=?`, nr.Name, nr.Color, nr.Permissions, b2i(nr.Hoist), b2i(nr.Mentionable), rid); err != nil {
		return nil, err
	}
	*r = nr
	return r, nil
}

// ReorderRoles sets positions from a list ordered highest first. @everyone
// is always position 0 and is left out.
func (s *Store) ReorderRoles(gid ID, order []ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return ErrNotFound
	}
	if len(order) != len(g.Roles)-1 {
		return errors.New("list every role except @everyone")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, rid := range order {
		if g.Roles[rid] == nil || rid == gid {
			return ErrNotFound
		}
		if _, err := tx.Exec(`UPDATE roles SET position = ? WHERE id = ?`, len(order)-i, rid); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for i, rid := range order {
		g.Roles[rid].Position = len(order) - i
	}
	return nil
}

func (s *Store) DeleteRole(gid, rid ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil || g.Roles[rid] == nil || rid == gid {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM roles WHERE id = ?`, rid); err != nil {
		return err
	}
	for _, m := range g.Members {
		if slices.Contains(m.Roles, rid) {
			roles := slices.DeleteFunc(slices.Clone(m.Roles), func(x ID) bool { return x == rid })
			if _, err := tx.Exec(`UPDATE members SET roles = ? WHERE guild_id = ? AND user_id = ?`, jsonStr(roles), gid, m.UserID); err != nil {
				return err
			}
		}
	}
	for _, c := range g.Channels {
		if slices.ContainsFunc(c.Overwrites, func(o Overwrite) bool { return o.ID == rid }) {
			ow := slices.DeleteFunc(slices.Clone(c.Overwrites), func(o Overwrite) bool { return o.ID == rid })
			if _, err := tx.Exec(`UPDATE channels SET overwrites = ? WHERE id = ?`, jsonStr(ow), c.ID); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	pos := g.Roles[rid].Position
	delete(g.Roles, rid)
	for _, r := range g.Roles {
		if r.Position > pos {
			r.Position--
		}
	}
	for _, m := range g.Members {
		m.Roles = slices.DeleteFunc(m.Roles, func(x ID) bool { return x == rid })
	}
	for _, c := range g.Channels {
		c.Overwrites = slices.DeleteFunc(c.Overwrites, func(o Overwrite) bool { return o.ID == rid })
	}
	return nil
}

// SortedRoles lists a server's roles highest first.
func (g *Guild) SortedRoles() []*Role {
	out := make([]*Role, 0, len(g.Roles))
	for _, r := range g.Roles {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position > out[j].Position })
	return out
}

func jsonStr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// --- channels ---

func insertChannel(e execer, c *Channel) error {
	if c.Overwrites == nil {
		c.Overwrites = []Overwrite{}
	}
	_, err := e.Exec(`INSERT INTO channels (id, guild_id, type, name, topic, position, parent_id, overwrites, bitrate, user_limit, slowmode, owner_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.GuildID, c.Type, c.Name, c.Topic, c.Position, c.ParentID, jsonStr(c.Overwrites), c.Bitrate, c.UserLimit, c.Slowmode, c.OwnerID)
	return err
}

func (s *Store) CreateChannel(gid ID, c Channel) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return nil, ErrNotFound
	}
	c.ID = NewID()
	c.GuildID = gid
	if c.ParentID != 0 {
		p := g.Channels[c.ParentID]
		if p == nil || p.Type != "category" || c.Type == "category" {
			return nil, errors.New("bad category")
		}
		// New channels start with their category's permissions.
		if c.Overwrites == nil {
			c.Overwrites = slices.Clone(p.Overwrites)
		}
	}
	if c.Type == "voice" && c.Bitrate == 0 {
		c.Bitrate = 96
	}
	max := -1
	for _, o := range g.Channels {
		if o.ParentID == c.ParentID && o.Position > max && (o.Type == "category") == (c.Type == "category") {
			max = o.Position
		}
	}
	c.Position = max + 1
	if err := insertChannel(s.db, &c); err != nil {
		return nil, err
	}
	g.Channels[c.ID] = &c
	s.channels[c.ID] = &c
	return &c, nil
}

type ChannelPatch struct {
	Name       *string      `json:"name"`
	Topic      *string      `json:"topic"`
	Position   *int         `json:"position"`
	ParentID   *ID          `json:"parent_id"`
	Overwrites *[]Overwrite `json:"overwrites"`
	Bitrate    *int         `json:"bitrate"`
	UserLimit  *int         `json:"user_limit"`
	Slowmode   *int         `json:"slowmode"`
}

func (s *Store) UpdateChannel(cid ID, p ChannelPatch) (*Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[cid]
	if c == nil {
		return nil, ErrNotFound
	}
	nc := *c
	set(&nc.Name, p.Name)
	set(&nc.Topic, p.Topic)
	set(&nc.Position, p.Position)
	set(&nc.ParentID, p.ParentID)
	set(&nc.Overwrites, p.Overwrites)
	set(&nc.Bitrate, p.Bitrate)
	set(&nc.UserLimit, p.UserLimit)
	set(&nc.Slowmode, p.Slowmode)
	if nc.ParentID != 0 {
		g := s.guilds[c.GuildID]
		if pc := g.Channels[nc.ParentID]; pc == nil || pc.Type != "category" || c.Type == "category" {
			return nil, errors.New("bad category")
		}
	}
	if nc.Overwrites == nil {
		nc.Overwrites = []Overwrite{}
	}
	if _, err := s.db.Exec(`UPDATE channels SET name=?, topic=?, position=?, parent_id=?, overwrites=?, bitrate=?, user_limit=?, slowmode=? WHERE id=?`,
		nc.Name, nc.Topic, nc.Position, nc.ParentID, jsonStr(nc.Overwrites), nc.Bitrate, nc.UserLimit, nc.Slowmode, cid); err != nil {
		return nil, err
	}
	*c = nc
	return c, nil
}

func (s *Store) DeleteChannel(cid ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[cid]
	if c == nil {
		return ErrNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, cid)
	tx.Exec(`DELETE FROM read_states WHERE channel_id = ?`, cid)
	tx.Exec(`UPDATE channels SET parent_id = 0 WHERE parent_id = ?`, cid)
	if _, err := tx.Exec(`DELETE FROM channels WHERE id = ?`, cid); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if g := s.guilds[c.GuildID]; g != nil {
		delete(g.Channels, cid)
		for _, o := range g.Channels {
			if o.ParentID == cid {
				o.ParentID = 0
			}
		}
	}
	delete(s.channels, cid)
	return nil
}

// OpenDM finds or makes the DM between exactly these users. More than two
// makes a group DM, which is always new.
func (s *Store) OpenDM(owner ID, others []ID) (*Channel, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(others) == 1 {
		for _, cid := range s.dms[owner] {
			c := s.channels[cid]
			if c.Type == "dm" && len(c.Recipients) == 2 && slices.Contains(c.Recipients, others[0]) {
				return c, false, nil
			}
		}
	}
	all := append([]ID{owner}, others...)
	c := &Channel{ID: NewID(), Type: "dm", Bitrate: 96, Recipients: all, Overwrites: []Overwrite{}}
	if len(all) > 2 {
		c.OwnerID = owner
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	if err := insertChannel(tx, c); err != nil {
		return nil, false, err
	}
	for _, uid := range all {
		if _, err := tx.Exec(`INSERT INTO dm_recipients (channel_id, user_id) VALUES (?, ?)`, c.ID, uid); err != nil {
			return nil, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	s.channels[c.ID] = c
	for _, uid := range all {
		s.dms[uid] = append(s.dms[uid], c.ID)
	}
	return c, true, nil
}

// LeaveDM removes a user from a group DM.
func (s *Store) LeaveDM(cid, uid ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.channels[cid]
	if c == nil || c.Type != "dm" || len(c.Recipients) < 3 {
		return ErrNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM dm_recipients WHERE channel_id = ? AND user_id = ?`, cid, uid); err != nil {
		return err
	}
	c.Recipients = slices.DeleteFunc(c.Recipients, func(x ID) bool { return x == uid })
	s.dms[uid] = slices.DeleteFunc(s.dms[uid], func(x ID) bool { return x == cid })
	if c.OwnerID == uid && len(c.Recipients) > 0 {
		c.OwnerID = c.Recipients[0]
		s.db.Exec(`UPDATE channels SET owner_id = ? WHERE id = ?`, c.OwnerID, cid)
	}
	return nil
}

// --- members ---

var ErrBanned = errors.New("you are banned from that server")

func (s *Store) AddMember(gid, uid ID) (*Member, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return nil, false, ErrNotFound
	}
	if g.Bans[uid] {
		return nil, false, ErrBanned
	}
	if m := g.Members[uid]; m != nil {
		return m, false, nil
	}
	m := &Member{GuildID: gid, UserID: uid, Roles: []ID{}, JoinedAt: now()}
	if _, err := s.db.Exec(`INSERT INTO members (guild_id, user_id, joined_at) VALUES (?, ?, ?)`, gid, uid, m.JoinedAt); err != nil {
		return nil, false, err
	}
	g.Members[uid] = m
	return m, true, nil
}

type MemberPatch struct {
	Nick  *string `json:"nick"`
	Roles *[]ID   `json:"roles"`
	Mute  *bool   `json:"mute"`
	Deaf  *bool   `json:"deaf"`
}

func (s *Store) UpdateMember(gid, uid ID, p MemberPatch) (*Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil || g.Members[uid] == nil {
		return nil, ErrNotFound
	}
	m := g.Members[uid]
	nm := *m
	set(&nm.Nick, p.Nick)
	set(&nm.Roles, p.Roles)
	set(&nm.Mute, p.Mute)
	set(&nm.Deaf, p.Deaf)
	nm.Roles = slices.DeleteFunc(slices.Clone(nm.Roles), func(r ID) bool { return g.Roles[r] == nil || r == gid })
	slices.Sort(nm.Roles)
	nm.Roles = slices.Compact(nm.Roles)
	if _, err := s.db.Exec(`UPDATE members SET nick=?, roles=?, mute=?, deaf=? WHERE guild_id=? AND user_id=?`, nm.Nick, jsonStr(nm.Roles), b2i(nm.Mute), b2i(nm.Deaf), gid, uid); err != nil {
		return nil, err
	}
	*m = nm
	return m, nil
}

func (s *Store) RemoveMember(gid, uid ID, ban bool, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return ErrNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM members WHERE guild_id = ? AND user_id = ?`, gid, uid); err != nil {
		return err
	}
	delete(g.Members, uid)
	if ban {
		if _, err := s.db.Exec(`INSERT OR REPLACE INTO bans (guild_id, user_id, reason) VALUES (?, ?, ?)`, gid, uid, reason); err != nil {
			return err
		}
		g.Bans[uid] = true
	}
	return nil
}

func (s *Store) Unban(gid, uid ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.guilds[gid]
	if g == nil {
		return ErrNotFound
	}
	if _, err := s.db.Exec(`DELETE FROM bans WHERE guild_id = ? AND user_id = ?`, gid, uid); err != nil {
		return err
	}
	delete(g.Bans, uid)
	return nil
}

type Ban struct {
	User   *User  `json:"user"`
	Reason string `json:"reason"`
}

func (s *Store) Bans(gid ID) ([]Ban, error) {
	rows, err := s.db.Query(`SELECT user_id, reason FROM bans WHERE guild_id = ?`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ban{}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for rows.Next() {
		var uid ID
		var b Ban
		if err := rows.Scan(&uid, &b.Reason); err != nil {
			return nil, err
		}
		if b.User = s.users[uid]; b.User != nil {
			out = append(out, b)
		}
	}
	return out, rows.Err()
}

// --- invites ---

type Invite struct {
	Code      string `json:"code"`
	GuildID   ID     `json:"guild_id"`
	CreatorID ID     `json:"creator_id"`
	Uses      int    `json:"uses"`
	MaxUses   int    `json:"max_uses"`
	ExpiresAt int64  `json:"expires_at"`
}

func (s *Store) CreateInvite(gid, creator ID, maxUses int, ttlMs int64) (*Invite, error) {
	inv := &Invite{Code: RandomToken(6), GuildID: gid, CreatorID: creator, MaxUses: maxUses}
	if ttlMs > 0 {
		inv.ExpiresAt = now() + ttlMs
	}
	_, err := s.db.Exec(`INSERT INTO invites (code, guild_id, creator_id, max_uses, expires_at) VALUES (?, ?, ?, ?, ?)`, inv.Code, gid, creator, maxUses, inv.ExpiresAt)
	return inv, err
}

// Invite looks up a usable invite.
func (s *Store) Invite(code string) (*Invite, error) {
	inv := &Invite{}
	err := s.db.QueryRow(`SELECT code, guild_id, creator_id, uses, max_uses, expires_at FROM invites WHERE code = ?`, code).Scan(&inv.Code, &inv.GuildID, &inv.CreatorID, &inv.Uses, &inv.MaxUses, &inv.ExpiresAt)
	if err != nil {
		return nil, ErrNotFound
	}
	if (inv.MaxUses > 0 && inv.Uses >= inv.MaxUses) || (inv.ExpiresAt > 0 && inv.ExpiresAt < now()) {
		return nil, ErrNotFound
	}
	return inv, nil
}

func (s *Store) UseInvite(code string) {
	s.db.Exec(`UPDATE invites SET uses = uses + 1 WHERE code = ?`, code)
}

func (s *Store) Invites(gid ID) ([]Invite, error) {
	rows, err := s.db.Query(`SELECT code, guild_id, creator_id, uses, max_uses, expires_at FROM invites WHERE guild_id = ? ORDER BY rowid DESC`, gid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var inv Invite
		if err := rows.Scan(&inv.Code, &inv.GuildID, &inv.CreatorID, &inv.Uses, &inv.MaxUses, &inv.ExpiresAt); err != nil {
			return nil, err
		}
		if inv.ExpiresAt > 0 && inv.ExpiresAt < now() {
			continue
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *Store) DeleteInvite(gid ID, code string) error {
	_, err := s.db.Exec(`DELETE FROM invites WHERE code = ? AND guild_id = ?`, code, gid)
	return err
}
