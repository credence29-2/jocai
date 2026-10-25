package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/credence29-2/jocai/victorcharlie/internal/store"
)

const maxMessage = 4000

var (
	userMention = regexp.MustCompile(`<@(\d+)>`)
	roleMention = regexp.MustCompile(`<@&(\d+)>`)
)

// mentions pulls pings out of message text. Call inside st.Read.
func mentions(content string, g *store.Guild, perms uint64) (users, roles []store.ID, everyone bool) {
	for _, m := range userMention.FindAllStringSubmatch(content, 20) {
		if id, err := store.ParseID(m[1]); err == nil && !slices.Contains(users, id) {
			users = append(users, id)
		}
	}
	for _, m := range roleMention.FindAllStringSubmatch(content, 20) {
		id, err := store.ParseID(m[1])
		if err != nil || g == nil || g.Roles[id] == nil || slices.Contains(roles, id) {
			continue
		}
		if g.Roles[id].Mentionable || perms&store.PermMentionEveryone != 0 {
			roles = append(roles, id)
		}
	}
	everyone = perms&store.PermMentionEveryone != 0 && (strings.Contains(content, "@everyone") || strings.Contains(content, "@here"))
	return
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var perms uint64
	s.st.Read(func() { _, _, perms = s.channelAccess(cid, u.ID) })
	if perms&store.PermReadHistory == 0 {
		fail(w, store.ErrNotFound)
		return
	}
	q := r.URL.Query()
	before, _ := store.ParseID(q.Get("before"))
	after, _ := store.ParseID(q.Get("after"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	msgs, err := s.st.Messages(cid, before, after, limit)
	if err != nil {
		fail(w, err)
		return
	}
	s.st.Read(func() { writeJSON(w, msgs) })
}

func (s *Server) postMessage(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var (
		c     *store.Channel
		g     *store.Guild
		perms uint64
		slow  int
	)
	s.st.Read(func() {
		c, g, perms = s.channelAccess(cid, u.ID)
		if c != nil {
			slow = c.Slowmode
		}
	})
	if c == nil || (c.Type != "text" && c.Type != "dm") {
		fail(w, store.ErrNotFound)
		return
	}
	if perms&store.PermSendMessages == 0 {
		forbidden(w)
		return
	}
	var body struct {
		Content string   `json:"content"`
		ReplyTo store.ID `json:"reply_to"`
		Nonce   string   `json:"nonce"`
	}
	// Rate checks come before reading the body so rejected uploads never
	// touch the disk.
	if slow > 0 && perms&(store.PermManageMessages|store.PermManageChannels) == 0 {
		key := [2]store.ID{cid, u.ID}
		s.slowMu.Lock()
		last := s.slow[key]
		if wait := time.Duration(slow)*time.Second - time.Since(last); wait > 0 {
			s.slowMu.Unlock()
			httpError(w, http.StatusTooManyRequests, "slow mode is on, wait "+strconv.Itoa(int(wait.Seconds())+1)+"s")
			return
		}
		s.slow[key] = time.Now()
		s.slowMu.Unlock()
	}
	if !s.limiter.allow("msg:"+u.ID.String(), 20, 10*time.Second) {
		httpError(w, http.StatusTooManyRequests, "you're sending messages too fast")
		return
	}
	var atts []store.Attachment
	// Files saved below are removed again if the message isn't posted.
	posted := false
	defer func() {
		if !posted {
			s.removeAttachments(atts)
		}
	}()
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if perms&store.PermAttachFiles == 0 {
			forbidden(w)
			return
		}
		var ok bool
		atts, ok = s.receiveFiles(w, r, u.ID)
		if !ok {
			return
		}
		json.Unmarshal([]byte(r.FormValue("payload_json")), &body)
	} else if !decode(w, r, &body) {
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if body.Content == "" && len(atts) == 0 {
		badRequest(w, "message is empty")
		return
	}
	if utf8.RuneCountInString(body.Content) > maxMessage {
		badRequest(w, "messages are up to 4000 characters")
		return
	}
	m := &store.Message{ChannelID: cid, Content: body.Content, Attachments: atts}
	if body.ReplyTo != 0 {
		if ref, err := s.st.Message(body.ReplyTo); err == nil && ref.ChannelID == cid {
			m.ReplyTo = ref.ID
			// Replying pings the person you replied to.
			m.Mentions = append(m.Mentions, ref.AuthorID())
		}
	}
	s.st.Read(func() {
		us, rs, all := mentions(body.Content, g, perms)
		for _, id := range us {
			if !slices.Contains(m.Mentions, id) {
				m.Mentions = append(m.Mentions, id)
			}
		}
		m.MentionRoles, m.MentionEveryone = rs, all
	})
	m, err := s.st.CreateMessage(m, u.ID)
	if err != nil {
		fail(w, err)
		return
	}
	posted = true
	out := struct {
		*store.Message
		Nonce string `json:"nonce,omitempty"`
	}{m, body.Nonce}
	s.toChannel(cid, "MESSAGE_CREATE", out)
	s.st.Read(func() { writeJSON(w, out) })
}

// ownMessage loads a message in a channel the caller can see.
func (s *Server) loadMessage(w http.ResponseWriter, r *http.Request, u *store.User) (*store.Message, *store.Guild, uint64, bool) {
	cid, mid := pathID(r, "cid"), pathID(r, "mid")
	var perms uint64
	var g *store.Guild
	s.st.Read(func() { _, g, perms = s.channelAccess(cid, u.ID) })
	if perms == 0 {
		fail(w, store.ErrNotFound)
		return nil, nil, 0, false
	}
	m, err := s.st.Message(mid)
	if err != nil || m.ChannelID != cid {
		fail(w, store.ErrNotFound)
		return nil, nil, 0, false
	}
	return m, g, perms, true
}

func (s *Server) editMessage(w http.ResponseWriter, r *http.Request, u *store.User) {
	m, g, perms, ok := s.loadMessage(w, r, u)
	if !ok {
		return
	}
	if m.AuthorID() != u.ID || m.Type != "default" {
		forbidden(w)
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &body) {
		return
	}
	body.Content = strings.TrimSpace(body.Content)
	if (body.Content == "" && len(m.Attachments) == 0) || utf8.RuneCountInString(body.Content) > maxMessage {
		badRequest(w, "messages are 1 to 4000 characters")
		return
	}
	var us, rs []store.ID
	var all bool
	s.st.Read(func() { us, rs, all = mentions(body.Content, g, perms) })
	if m.Referenced != nil && !slices.Contains(us, m.Referenced.Author.ID) {
		us = append(us, m.Referenced.Author.ID)
	}
	nm, err := s.st.EditMessage(m.ID, body.Content, us, rs, all)
	if err != nil {
		fail(w, err)
		return
	}
	s.toChannel(m.ChannelID, "MESSAGE_UPDATE", nm)
	s.st.Read(func() { writeJSON(w, nm) })
}

func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request, u *store.User) {
	m, _, perms, ok := s.loadMessage(w, r, u)
	if !ok {
		return
	}
	if m.AuthorID() != u.ID && perms&store.PermManageMessages == 0 {
		forbidden(w)
		return
	}
	if err := s.st.DeleteMessage(m.ID); err != nil {
		fail(w, err)
		return
	}
	s.removeAttachments(m.Attachments)
	s.toChannel(m.ChannelID, "MESSAGE_DELETE", map[string]any{"id": m.ID, "channel_id": m.ChannelID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) addReaction(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.react(w, r, u, true)
}

func (s *Server) removeReaction(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.react(w, r, u, false)
}

func (s *Server) react(w http.ResponseWriter, r *http.Request, u *store.User, add bool) {
	m, _, perms, ok := s.loadMessage(w, r, u)
	if !ok {
		return
	}
	emoji, err := url.PathUnescape(r.PathValue("emoji"))
	if err != nil || emoji == "" || len(emoji) > 64 {
		badRequest(w, "bad emoji")
		return
	}
	var changed bool
	if add {
		if perms&store.PermAddReactions == 0 {
			forbidden(w)
			return
		}
		existing := false
		for _, rx := range m.Reactions {
			if rx.Emoji == emoji {
				existing = true
			}
		}
		if !existing && len(m.Reactions) >= 20 {
			badRequest(w, "that message has as many different reactions as it can hold")
			return
		}
		changed, err = s.st.AddReaction(m.ID, u.ID, emoji)
	} else {
		changed, err = s.st.RemoveReaction(m.ID, u.ID, emoji)
	}
	if err != nil {
		fail(w, err)
		return
	}
	if changed {
		t := "REACTION_ADD"
		if !add {
			t = "REACTION_REMOVE"
		}
		s.toChannel(m.ChannelID, t, map[string]any{"message_id": m.ID, "channel_id": m.ChannelID, "user_id": u.ID, "emoji": emoji})
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listPins(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var perms uint64
	s.st.Read(func() { _, _, perms = s.channelAccess(cid, u.ID) })
	if perms&store.PermReadHistory == 0 {
		fail(w, store.ErrNotFound)
		return
	}
	pins, err := s.st.Pins(cid)
	if err != nil {
		fail(w, err)
		return
	}
	s.st.Read(func() { writeJSON(w, pins) })
}

func (s *Server) pin(w http.ResponseWriter, r *http.Request, u *store.User) { s.setPin(w, r, u, true) }
func (s *Server) unpin(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.setPin(w, r, u, false)
}

func (s *Server) setPin(w http.ResponseWriter, r *http.Request, u *store.User, on bool) {
	m, g, perms, ok := s.loadMessage(w, r, u)
	if !ok {
		return
	}
	// Anyone can pin in a DM; servers need Manage Messages.
	if g != nil && perms&store.PermManageMessages == 0 {
		forbidden(w)
		return
	}
	if err := s.st.SetPinned(m.ID, on); err != nil {
		fail(w, err)
		return
	}
	m.Pinned = on
	s.toChannel(m.ChannelID, "MESSAGE_UPDATE", m)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) typing(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var perms uint64
	s.st.Read(func() { _, _, perms = s.channelAccess(cid, u.ID) })
	if perms&store.PermSendMessages == 0 {
		forbidden(w)
		return
	}
	s.toChannel(cid, "TYPING", map[string]any{"channel_id": cid, "user_id": u.ID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ack(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var body struct {
		MessageID store.ID `json:"message_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	var perms uint64
	s.st.Read(func() { _, _, perms = s.channelAccess(cid, u.ID) })
	if perms == 0 {
		fail(w, store.ErrNotFound)
		return
	}
	if err := s.st.Ack(u.ID, cid, body.MessageID); err != nil {
		fail(w, err)
		return
	}
	// Other devices clear their unread badge too.
	s.hub.toUsers([]store.ID{u.ID}, "READ_STATE", map[string]any{"channel_id": cid, "last_read_id": body.MessageID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request, u *store.User) {
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("q"))
	gid, _ := store.ParseID(q.Get("guild_id"))
	cid, _ := store.ParseID(q.Get("channel_id"))
	author, _ := store.ParseID(q.Get("author_id"))
	if text == "" && author == 0 {
		writeJSON(w, []any{})
		return
	}
	var cids []store.ID
	s.st.Read(func() {
		if cid != 0 {
			if _, _, p := s.channelAccess(cid, u.ID); p&store.PermReadHistory != 0 {
				cids = append(cids, cid)
			}
			return
		}
		if g := s.st.Guild(gid); g != nil && g.Members[u.ID] != nil {
			for _, c := range g.Channels {
				if c.Type == "text" && g.In(u.ID, c)&store.PermReadHistory != 0 {
					cids = append(cids, c.ID)
				}
			}
		}
	})
	msgs, err := s.st.Search(cids, text, author, 25)
	if err != nil {
		fail(w, err)
		return
	}
	s.st.Read(func() { writeJSON(w, msgs) })
}
