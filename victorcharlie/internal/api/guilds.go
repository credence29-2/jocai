package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/credence29-2/jocai/victorcharlie/internal/store"
)

// access looks up a server and what uid may do there. g is nil if uid isn't
// a member. Call inside st.Read.
func (s *Server) access(gid, uid store.ID) (*store.Guild, uint64) {
	g := s.st.Guild(gid)
	if g == nil || g.Members[uid] == nil {
		return nil, 0
	}
	return g, g.Base(uid)
}

// check runs fn under the read lock with the server and the caller's base
// permissions, and fails the request if they lack need.
func (s *Server) check(w http.ResponseWriter, gid, uid store.ID, need uint64, fn func(g *store.Guild, perms uint64) bool) bool {
	ok, found := true, true
	s.st.Read(func() {
		g, p := s.access(gid, uid)
		if g == nil {
			found = false
			return
		}
		if p&need != need {
			ok = false
			return
		}
		if fn != nil {
			ok = fn(g, p)
		}
	})
	if !found {
		fail(w, store.ErrNotFound)
		return false
	}
	if !ok {
		forbidden(w)
		return false
	}
	return true
}

func cleanName(n string, max int) (string, bool) {
	n = strings.TrimSpace(n)
	return n, n != "" && utf8.RuneCountInString(n) <= max
}

func (s *Server) joinGuild(inv *store.Invite, uid store.ID) error {
	_, added, err := s.st.AddMember(inv.GuildID, uid)
	if err != nil {
		return err
	}
	if !added {
		return nil
	}
	s.st.UseInvite(inv.Code)
	var sys store.ID
	s.st.Read(func() {
		if g := s.st.Guild(inv.GuildID); g != nil {
			sys = g.SystemChannelID
		}
	})
	s.syncGuild(inv.GuildID)
	s.broadcastPresence(uid)
	if sys != 0 {
		if m, err := s.st.CreateMessage(&store.Message{ChannelID: sys, Type: "join"}, uid); err == nil {
			s.toChannel(sys, "MESSAGE_CREATE", m)
		}
	}
	return nil
}

func (s *Server) getInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.st.Invite(r.PathValue("code"))
	if err != nil {
		fail(w, err)
		return
	}
	var out map[string]any
	s.st.Read(func() {
		g := s.st.Guild(inv.GuildID)
		if g == nil {
			return
		}
		var inviter string
		if u := s.st.User(inv.CreatorID); u != nil {
			inviter = u.DisplayName
		}
		out = map[string]any{"code": inv.Code, "guild": map[string]any{"id": g.ID, "name": g.Name, "icon": g.Icon}, "members": len(g.Members), "inviter": inviter}
	})
	if out == nil {
		fail(w, store.ErrNotFound)
		return
	}
	writeJSON(w, out)
}

func (s *Server) acceptInvite(w http.ResponseWriter, r *http.Request, u *store.User) {
	inv, err := s.st.Invite(r.PathValue("code"))
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.joinGuild(inv, u.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"guild_id": inv.GuildID})
}

func (s *Server) createGuild(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	name, ok := cleanName(body.Name, 100)
	if !ok {
		badRequest(w, "server names are 1 to 100 characters")
		return
	}
	g, err := s.st.CreateGuild(u.ID, name)
	if err != nil {
		fail(w, err)
		return
	}
	s.syncGuild(g.ID)
	writeJSON(w, map[string]any{"id": g.ID})
}

func (s *Server) patchGuild(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var p store.GuildPatch
	if !decode(w, r, &p) {
		return
	}
	p.Icon = nil
	if p.Name != nil {
		n, ok := cleanName(*p.Name, 100)
		if !ok {
			badRequest(w, "server names are 1 to 100 characters")
			return
		}
		p.Name = &n
	}
	if !s.check(w, gid, u.ID, store.PermManageServer, func(g *store.Guild, _ uint64) bool {
		if p.OwnerID != nil && (u.ID != g.OwnerID || g.Members[*p.OwnerID] == nil) {
			return false
		}
		for _, cid := range []*store.ID{p.AFKChannelID, p.SystemChannelID} {
			if cid != nil && *cid != 0 && g.Channels[*cid] == nil {
				return false
			}
		}
		return true
	}) {
		return
	}
	if err := s.st.UpdateGuild(gid, p); err != nil {
		fail(w, err)
		return
	}
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) uploadGuildIcon(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	if !s.check(w, gid, u.ID, store.PermManageServer, nil) {
		return
	}
	a, ok := s.receiveImage(w, r, u.ID)
	if !ok {
		return
	}
	var old string
	s.st.Read(func() { old = s.st.Guild(gid).Icon })
	if err := s.st.UpdateGuild(gid, store.GuildPatch{Icon: &a.URL}); err != nil {
		s.removeAttachments([]store.Attachment{a})
		fail(w, err)
		return
	}
	s.removeAttachments([]store.Attachment{{URL: old}})
	s.syncGuild(gid)
	writeJSON(w, map[string]string{"icon": a.URL})
}

func (s *Server) deleteGuild(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var members []store.ID
	if !s.check(w, gid, u.ID, 0, func(g *store.Guild, _ uint64) bool {
		for m := range g.Members {
			members = append(members, m)
		}
		return g.OwnerID == u.ID
	}) {
		return
	}
	for _, v := range s.hub.voiceInGuild(gid) {
		s.removeFromRoom(v.ChannelID, v.UserID)
	}
	var cids []store.ID
	var icon string
	s.st.Read(func() {
		g := s.st.Guild(gid)
		icon = g.Icon
		for cid := range g.Channels {
			cids = append(cids, cid)
		}
	})
	files := s.st.AttachmentsIn(cids)
	if err := s.st.DeleteGuild(gid); err != nil {
		fail(w, err)
		return
	}
	s.removeAttachments(append(files, store.Attachment{URL: icon}))
	s.hub.toUsers(members, "GUILD_DELETE", map[string]any{"id": gid})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) voiceInGuild(gid store.ID) []VoiceState {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []VoiceState
	for _, v := range h.voice {
		if v.GuildID == gid {
			out = append(out, *v)
		}
	}
	return out
}

func (s *Server) leaveGuild(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	if !s.check(w, gid, u.ID, 0, func(g *store.Guild, _ uint64) bool { return g.OwnerID != u.ID }) {
		return
	}
	s.removeMember(gid, u.ID, false, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) removeMember(gid, uid store.ID, ban bool, reason string) error {
	s.hub.mu.Lock()
	v := s.hub.voice[uid]
	s.hub.mu.Unlock()
	if err := s.st.RemoveMember(gid, uid, ban, reason); err != nil {
		return err
	}
	if v != nil && v.GuildID == gid {
		s.removeFromRoom(v.ChannelID, uid)
	}
	s.hub.toUsers([]store.ID{uid}, "GUILD_DELETE", map[string]any{"id": gid})
	s.syncGuild(gid)
	return nil
}

func (s *Server) patchMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, uid := pathID(r, "gid"), pathID(r, "uid")
	if r.PathValue("uid") == "@me" {
		uid = u.ID
	}
	var raw map[string]json.RawMessage
	if !decode(w, r, &raw) {
		return
	}
	var p store.MemberPatch
	b, _ := json.Marshal(raw)
	json.Unmarshal(b, &p)
	// channel_id moves someone to another voice channel; null disconnects.
	moveRaw, move := raw["channel_id"]
	var moveTo store.ID
	if move {
		json.Unmarshal(moveRaw, &moveTo)
	}
	if p.Nick != nil {
		n := strings.TrimSpace(*p.Nick)
		if utf8.RuneCountInString(n) > 32 {
			badRequest(w, "nicknames are up to 32 characters")
			return
		}
		p.Nick = &n
	}
	if !s.check(w, gid, u.ID, 0, func(g *store.Guild, perms uint64) bool {
		m := g.Members[uid]
		if m == nil {
			return false
		}
		self := uid == u.ID
		outranks := g.Outranks(u.ID, uid)
		if p.Nick != nil {
			if self && perms&(store.PermChangeNickname|store.PermManageNicknames) == 0 {
				return false
			}
			if !self && (perms&store.PermManageNicknames == 0 || !outranks) {
				return false
			}
		}
		if p.Roles != nil {
			if perms&store.PermManageRoles == 0 {
				return false
			}
			// Only roles below your own may be given or taken.
			top := g.TopRole(u.ID)
			was := map[store.ID]bool{}
			for _, rid := range m.Roles {
				was[rid] = true
			}
			now := map[store.ID]bool{}
			for _, rid := range *p.Roles {
				now[rid] = true
			}
			for rid := range was {
				if !now[rid] && g.Roles[rid] != nil && g.Roles[rid].Position >= top {
					return false
				}
			}
			for rid := range now {
				if !was[rid] && g.Roles[rid] != nil && g.Roles[rid].Position >= top {
					return false
				}
			}
			if !self && !outranks {
				return false
			}
		}
		if p.Mute != nil && (perms&store.PermMuteMembers == 0 || (!self && !outranks)) {
			return false
		}
		if p.Deaf != nil && (perms&store.PermDeafenMembers == 0 || (!self && !outranks)) {
			return false
		}
		if move {
			if perms&store.PermMoveMembers == 0 && !(self && moveTo == 0) {
				return false
			}
			if moveTo != 0 {
				c := g.Channels[moveTo]
				if c == nil || c.Type != "voice" || g.In(u.ID, c)&store.PermConnect == 0 || g.In(uid, c)&store.PermConnect == 0 {
					return false
				}
			}
		}
		return true
	}) {
		return
	}
	if p.Nick != nil || p.Roles != nil || p.Mute != nil || p.Deaf != nil {
		if _, err := s.st.UpdateMember(gid, uid, p); err != nil {
			fail(w, err)
			return
		}
		s.syncGuild(gid)
	}
	if move {
		s.hub.mu.Lock()
		v := s.hub.voice[uid]
		s.hub.mu.Unlock()
		if v != nil && v.GuildID == gid {
			if moveTo == 0 {
				s.removeFromRoom(v.ChannelID, uid)
			} else {
				s.hub.toUsers([]store.ID{uid}, "VOICE_MOVE", map[string]any{"channel_id": moveTo, "guild_id": gid})
			}
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) kickMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, uid := pathID(r, "gid"), pathID(r, "uid")
	if !s.check(w, gid, u.ID, store.PermKickMembers, func(g *store.Guild, _ uint64) bool {
		return g.Members[uid] != nil && uid != u.ID && g.Outranks(u.ID, uid)
	}) {
		return
	}
	if err := s.removeMember(gid, uid, false, ""); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listBans(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	if !s.check(w, gid, u.ID, store.PermBanMembers, nil) {
		return
	}
	bans, err := s.st.Bans(gid)
	if err != nil {
		fail(w, err)
		return
	}
	s.st.Read(func() { writeJSON(w, bans) })
}

func (s *Server) banMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, uid := pathID(r, "gid"), pathID(r, "uid")
	var body struct {
		Reason string `json:"reason"`
	}
	decode(w, r, &body)
	if !s.check(w, gid, u.ID, store.PermBanMembers, func(g *store.Guild, _ uint64) bool {
		return uid != u.ID && s.st.User(uid) != nil && (g.Members[uid] == nil || g.Outranks(u.ID, uid))
	}) {
		return
	}
	if err := s.removeMember(gid, uid, true, body.Reason); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) unbanMember(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, uid := pathID(r, "gid"), pathID(r, "uid")
	if !s.check(w, gid, u.ID, store.PermBanMembers, nil) {
		return
	}
	if err := s.st.Unban(gid, uid); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- roles ---

func (s *Server) createRole(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var body store.Role
	if !decode(w, r, &body) {
		return
	}
	name, ok := cleanName(body.Name, 100)
	if !ok {
		name = "new role"
	}
	body.Name = name
	if !s.check(w, gid, u.ID, store.PermManageRoles, func(g *store.Guild, perms uint64) bool {
		return body.Permissions&^perms == 0
	}) {
		return
	}
	role, err := s.st.CreateRole(gid, body)
	if err != nil {
		fail(w, err)
		return
	}
	s.syncGuild(gid)
	s.st.Read(func() { writeJSON(w, role) })
}

func (s *Server) patchRole(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, rid := pathID(r, "gid"), pathID(r, "rid")
	var p store.RolePatch
	if !decode(w, r, &p) {
		return
	}
	if p.Name != nil {
		n, ok := cleanName(*p.Name, 100)
		if !ok {
			badRequest(w, "role names are 1 to 100 characters")
			return
		}
		p.Name = &n
	}
	if !s.check(w, gid, u.ID, store.PermManageRoles, func(g *store.Guild, perms uint64) bool {
		role := g.Roles[rid]
		if role == nil {
			return false
		}
		if rid != gid && role.Position >= g.TopRole(u.ID) {
			return false
		}
		// You can't hand out permissions you don't have yourself.
		if p.Permissions != nil && (*p.Permissions^role.Permissions)&^perms != 0 {
			return false
		}
		return true
	}) {
		return
	}
	role, err := s.st.UpdateRole(gid, rid, p)
	if err != nil {
		fail(w, err)
		return
	}
	s.syncGuild(gid)
	s.st.Read(func() { writeJSON(w, role) })
}

func (s *Server) reorderRoles(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var order []store.ID
	if !decode(w, r, &order) {
		return
	}
	if !s.check(w, gid, u.ID, store.PermManageRoles, func(g *store.Guild, _ uint64) bool {
		if u.ID == g.OwnerID {
			return true
		}
		// Roles at or above yours must keep their order and stay on top.
		top := g.TopRole(u.ID)
		var locked []store.ID
		for _, role := range g.SortedRoles() {
			if role.Position >= top && role.ID != gid {
				locked = append(locked, role.ID)
			}
		}
		if len(order) < len(locked) {
			return false
		}
		for i, id := range locked {
			if order[i] != id {
				return false
			}
		}
		return true
	}) {
		return
	}
	if err := s.st.ReorderRoles(gid, order); err != nil {
		badRequest(w, err.Error())
		return
	}
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid, rid := pathID(r, "gid"), pathID(r, "rid")
	if !s.check(w, gid, u.ID, store.PermManageRoles, func(g *store.Guild, _ uint64) bool {
		role := g.Roles[rid]
		return role != nil && rid != gid && role.Position < g.TopRole(u.ID)
	}) {
		return
	}
	if err := s.st.DeleteRole(gid, rid); err != nil {
		fail(w, err)
		return
	}
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

// --- channels ---

func validBitrate(b int) bool { return b >= 8 && b <= 384 }

func (s *Server) createChannel(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var c store.Channel
	if !decode(w, r, &c) {
		return
	}
	switch c.Type {
	case "text":
		c.Name = slugify(c.Name)
	case "voice", "category":
		c.Name = strings.TrimSpace(c.Name)
	default:
		badRequest(w, "channel type must be text, voice or category")
		return
	}
	if c.Name == "" || utf8.RuneCountInString(c.Name) > 100 {
		badRequest(w, "channel names are 1 to 100 characters")
		return
	}
	if c.Bitrate != 0 && !validBitrate(c.Bitrate) {
		badRequest(w, "bitrate must be 8 to 384 kbps")
		return
	}
	c.OwnerID, c.Recipients, c.LastMessageID = 0, nil, 0
	if !s.check(w, gid, u.ID, store.PermManageChannels, func(g *store.Guild, base uint64) bool {
		if len(c.Overwrites) == 0 {
			return true
		}
		// Same rule as editing a channel's permissions.
		if base&store.PermManageRoles == 0 {
			return false
		}
		for _, o := range c.Overwrites {
			if (o.Allow|o.Deny)&^base != 0 {
				return false
			}
		}
		return true
	}) {
		return
	}
	nc, err := s.st.CreateChannel(gid, c)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	s.syncGuild(gid)
	s.st.Read(func() { writeJSON(w, nc) })
}

func slugify(n string) string {
	n = strings.ToLower(strings.TrimSpace(n))
	var b strings.Builder
	dash := false
	for _, r := range n {
		switch {
		case r == ' ' || r == '-' || r == '_':
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		case r == '#' || r == '@' || r == '<' || r == '>' || r == '/' || r == '\\':
		default:
			b.WriteRune(r)
			dash = false
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// channelAccess resolves a channel and the caller's permissions in it.
// Call inside st.Read.
func (s *Server) channelAccess(cid, uid store.ID) (*store.Channel, *store.Guild, uint64) {
	c := s.st.Channel(cid)
	if c == nil {
		return nil, nil, 0
	}
	if c.Type == "dm" {
		for _, r := range c.Recipients {
			if r == uid {
				p := store.PermViewChannel | store.PermSendMessages | store.PermAttachFiles | store.PermAddReactions | store.PermReadHistory | store.PermMentionEveryone
				return c, nil, p
			}
		}
		return nil, nil, 0
	}
	g := s.st.Guild(c.GuildID)
	if g == nil || g.Members[uid] == nil {
		return nil, nil, 0
	}
	p := g.In(uid, c)
	if p&store.PermViewChannel == 0 {
		return nil, nil, 0
	}
	return c, g, p
}

func (s *Server) patchChannel(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var p store.ChannelPatch
	if !decode(w, r, &p) {
		return
	}
	var gid store.ID
	var isText bool
	ok := false
	s.st.Read(func() {
		c, g, perms := s.channelAccess(cid, u.ID)
		if c == nil || g == nil {
			return
		}
		gid = g.ID
		isText = c.Type == "text"
		ok = perms&store.PermManageChannels != 0
		if p.Overwrites != nil {
			// Changing who can see a channel needs Manage Roles, and you can't
			// grant what you don't have.
			base := g.Base(u.ID)
			ok = ok && base&store.PermManageRoles != 0
			for _, o := range *p.Overwrites {
				if (o.Allow|o.Deny)&^base != 0 {
					ok = false
				}
			}
		}
	})
	if gid == 0 {
		fail(w, store.ErrNotFound)
		return
	}
	if !ok {
		forbidden(w)
		return
	}
	if p.Name != nil {
		n := strings.TrimSpace(*p.Name)
		if isText {
			n = slugify(n)
		}
		if n == "" || utf8.RuneCountInString(n) > 100 {
			badRequest(w, "channel names are 1 to 100 characters")
			return
		}
		p.Name = &n
	}
	if p.Topic != nil && utf8.RuneCountInString(*p.Topic) > 1024 {
		badRequest(w, "topics are up to 1024 characters")
		return
	}
	if p.Bitrate != nil && !validBitrate(*p.Bitrate) {
		badRequest(w, "bitrate must be 8 to 384 kbps")
		return
	}
	if _, err := s.st.UpdateChannel(cid, p); err != nil {
		badRequest(w, err.Error())
		return
	}
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) reorderChannels(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var moves []struct {
		ID       store.ID  `json:"id"`
		Position int       `json:"position"`
		ParentID *store.ID `json:"parent_id"`
	}
	if !decode(w, r, &moves) {
		return
	}
	if !s.check(w, gid, u.ID, store.PermManageChannels, func(g *store.Guild, _ uint64) bool {
		for _, m := range moves {
			if g.Channels[m.ID] == nil {
				return false
			}
		}
		return true
	}) {
		return
	}
	for _, m := range moves {
		pos := m.Position
		if _, err := s.st.UpdateChannel(m.ID, store.ChannelPatch{Position: &pos, ParentID: m.ParentID}); err != nil {
			badRequest(w, err.Error())
			return
		}
	}
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteChannel(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var gid store.ID
	ok := false
	s.st.Read(func() {
		c, g, perms := s.channelAccess(cid, u.ID)
		if c != nil && g != nil {
			gid = g.ID
			ok = perms&store.PermManageChannels != 0
		}
	})
	if gid == 0 {
		fail(w, store.ErrNotFound)
		return
	}
	if !ok {
		forbidden(w)
		return
	}
	for _, v := range s.hub.voiceIn(cid) {
		s.removeFromRoom(cid, v.UserID)
	}
	files := s.st.AttachmentsIn([]store.ID{cid})
	if err := s.st.DeleteChannel(cid); err != nil {
		fail(w, err)
		return
	}
	s.removeAttachments(files)
	s.syncGuild(gid)
	w.WriteHeader(http.StatusNoContent)
}

// --- invites ---

func (s *Server) listInvites(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	if !s.check(w, gid, u.ID, store.PermManageServer, nil) {
		return
	}
	inv, err := s.st.Invites(gid)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, inv)
}

func (s *Server) createInvite(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	var body struct {
		MaxUses  int `json:"max_uses"`
		MaxHours int `json:"max_hours"`
	}
	decode(w, r, &body)
	if !s.check(w, gid, u.ID, store.PermCreateInvite, nil) {
		return
	}
	inv, err := s.st.CreateInvite(gid, u.ID, max(body.MaxUses, 0), int64(max(body.MaxHours, 0))*int64(time.Hour/time.Millisecond))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, inv)
}

func (s *Server) deleteInvite(w http.ResponseWriter, r *http.Request, u *store.User) {
	gid := pathID(r, "gid")
	if !s.check(w, gid, u.ID, store.PermManageServer, nil) {
		return
	}
	if err := s.st.DeleteInvite(gid, r.PathValue("code")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
