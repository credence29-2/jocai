package api

import (
	"context"
	"log"
	"net/http"
	"slices"

	"github.com/credence29-2/jocai/victorcharlie/internal/media"
	"github.com/credence29-2/jocai/victorcharlie/internal/store"
	"github.com/livekit/protocol/livekit"
)

// voicePerms works out what uid may do in a voice channel. Call inside
// st.Read. ok is false when they can't join at all.
func (s *Server) voicePerms(uid store.ID, c *store.Channel) (perms uint64, mute, deaf, ok bool) {
	if c.Type == "dm" {
		return store.PermAll, false, false, slices.Contains(c.Recipients, uid)
	}
	if c.Type != "voice" {
		return 0, false, false, false
	}
	g := s.st.Guild(c.GuildID)
	if g == nil || g.Members[uid] == nil {
		return 0, false, false, false
	}
	p := g.In(uid, c)
	m := g.Members[uid]
	return p, m.Mute, m.Deaf, p&store.PermConnect != 0
}

func sources(perms uint64, mute bool) []string {
	var out []string
	if perms&store.PermSpeak != 0 && !mute {
		out = append(out, "microphone")
	}
	if perms&store.PermVideo != 0 {
		out = append(out, "camera", "screen_share", "screen_share_audio")
	}
	return out
}

func lkSources(names []string) []livekit.TrackSource {
	var out []livekit.TrackSource
	for _, n := range names {
		switch n {
		case "microphone":
			out = append(out, livekit.TrackSource_MICROPHONE)
		case "camera":
			out = append(out, livekit.TrackSource_CAMERA)
		case "screen_share":
			out = append(out, livekit.TrackSource_SCREEN_SHARE)
		case "screen_share_audio":
			out = append(out, livekit.TrackSource_SCREEN_SHARE_AUDIO)
		}
	}
	return out
}

func (s *Server) voiceJoin(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var body struct {
		SelfMute bool `json:"self_mute"`
		SelfDeaf bool `json:"self_deaf"`
	}
	decode(w, r, &body)
	var (
		perms          uint64
		mute, deaf, ok bool
		full           bool
		name           string
		gid            store.ID
	)
	s.st.Read(func() {
		c := s.st.Channel(cid)
		if c == nil {
			return
		}
		perms, mute, deaf, ok = s.voicePerms(u.ID, c)
		gid = c.GuildID
		name = u.DisplayName
		if g := s.st.Guild(gid); g != nil {
			if m := g.Members[u.ID]; m != nil && m.Nick != "" {
				name = m.Nick
			}
		}
		if ok && c.UserLimit > 0 && perms&store.PermMoveMembers == 0 {
			n := 0
			for _, v := range s.hub.voiceIn(cid) {
				if v.UserID != u.ID {
					n++
				}
			}
			full = n >= c.UserLimit
		}
	})
	if !ok {
		forbidden(w)
		return
	}
	if full {
		httpError(w, http.StatusConflict, "that channel is full")
		return
	}
	src := sources(perms, mute)
	tok, err := media.Token(s.cfg.LiveKitKey, s.cfg.LiveKitSecret, u.ID.String(), name, cid.String(), len(src) > 0, !deaf, src)
	if err != nil {
		fail(w, err)
		return
	}
	// One voice channel at a time: drop any other connection.
	s.hub.mu.Lock()
	old := s.hub.voice[u.ID]
	s.hub.pending[u.ID] = VoiceState{SelfMute: body.SelfMute, SelfDeaf: body.SelfDeaf}
	s.hub.mu.Unlock()
	if old != nil && old.ChannelID != cid {
		s.removeFromRoom(old.ChannelID, u.ID)
	}
	writeJSON(w, map[string]any{
		"token":       tok,
		"permissions": perms,
		"bitrate":     s.bitrate(cid),
		"use_vad":     perms&store.PermUseVAD != 0,
		"guild_id":    gid,
	})
}

func (s *Server) bitrate(cid store.ID) int {
	b := 96
	s.st.Read(func() {
		if c := s.st.Channel(cid); c != nil && c.Bitrate > 0 {
			b = c.Bitrate
		}
	})
	return b
}

// voiceRing tells the other people in a DM that a call started.
func (s *Server) voiceRing(w http.ResponseWriter, r *http.Request, u *store.User) {
	cid := pathID(r, "cid")
	var others []store.ID
	s.st.Read(func() {
		c := s.st.Channel(cid)
		if c == nil || c.Type != "dm" || !slices.Contains(c.Recipients, u.ID) {
			return
		}
		for _, id := range c.Recipients {
			if id != u.ID {
				others = append(others, id)
			}
		}
	})
	if others == nil {
		forbidden(w)
		return
	}
	s.hub.toUsers(others, "CALL_RING", map[string]any{"channel_id": cid, "user_id": u.ID})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminCtx(room store.ID) context.Context {
	ctx, err := media.AdminContext(context.Background(), s.cfg.LiveKitKey, s.cfg.LiveKitSecret, room.String())
	if err != nil {
		log.Printf("media token: %v", err)
	}
	return ctx
}

func (s *Server) removeFromRoom(cid, uid store.ID) {
	_, err := s.media.Rooms.RemoveParticipant(s.adminCtx(cid), &livekit.RoomParticipantIdentity{Room: cid.String(), Identity: uid.String()})
	if err != nil {
		log.Printf("remove %v from %v: %v", uid, cid, err)
	}
}

// disconnectVoice drops a user from whatever voice channel they are in.
func (s *Server) disconnectVoice(uid store.ID) {
	s.hub.mu.Lock()
	v := s.hub.voice[uid]
	s.hub.mu.Unlock()
	if v != nil {
		s.removeFromRoom(v.ChannelID, uid)
	}
}

// refreshVoicePerms pushes role and server-mute changes to people already in
// voice, and drops anyone who lost access.
func (s *Server) refreshVoicePerms(gid store.ID) {
	s.hub.mu.Lock()
	var vs []VoiceState
	for _, v := range s.hub.voice {
		if v.GuildID == gid {
			vs = append(vs, *v)
		}
	}
	s.hub.mu.Unlock()
	for _, v := range vs {
		var perms uint64
		var mute, deaf, ok bool
		s.st.Read(func() {
			if c := s.st.Channel(v.ChannelID); c != nil {
				perms, mute, deaf, ok = s.voicePerms(v.UserID, c)
			}
		})
		if !ok {
			s.removeFromRoom(v.ChannelID, v.UserID)
			continue
		}
		src := sources(perms, mute)
		_, err := s.media.Rooms.UpdateParticipant(s.adminCtx(v.ChannelID), &livekit.UpdateParticipantRequest{
			Room:     v.ChannelID.String(),
			Identity: v.UserID.String(),
			Permission: &livekit.ParticipantPermission{
				CanSubscribe:      !deaf,
				CanPublish:        len(src) > 0,
				CanPublishData:    true,
				CanPublishSources: lkSources(src),
			},
		})
		if err != nil {
			log.Printf("update voice perms for %v: %v", v.UserID, err)
		}
		if v.Mute != mute || v.Deaf != deaf {
			s.hub.mu.Lock()
			if cur := s.hub.voice[v.UserID]; cur != nil {
				cur.Mute, cur.Deaf = mute, deaf
				v = *cur
			}
			s.hub.mu.Unlock()
			s.broadcastVoice(v)
		}
	}
}

func (h *Hub) voiceIn(cid store.ID) []VoiceState {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []VoiceState
	for _, v := range h.voice {
		if v.ChannelID == cid {
			out = append(out, *v)
		}
	}
	return out
}

func (s *Server) visibleVoice(uid store.ID) []*VoiceState {
	s.hub.mu.Lock()
	all := make([]VoiceState, 0, len(s.hub.voice))
	for _, v := range s.hub.voice {
		all = append(all, *v)
	}
	s.hub.mu.Unlock()
	out := []*VoiceState{}
	s.st.Read(func() {
		for _, v := range all {
			c := s.st.Channel(v.ChannelID)
			if c == nil {
				continue
			}
			if c.Type == "dm" {
				if slices.Contains(c.Recipients, uid) {
					out = append(out, &v)
				}
			} else if g := s.st.Guild(c.GuildID); g != nil && g.In(uid, c)&store.PermViewChannel != 0 {
				out = append(out, &v)
			}
		}
	})
	return out
}

// broadcastVoice tells everyone who can see the channel about a voice state.
// A zero ChannelID means the user left; it goes to whoever could see the
// channel they left (prev).
func (s *Server) broadcastVoice(v VoiceState) {
	s.toChannel(v.ChannelID, "VOICE_STATE", v)
}

func (s *Server) broadcastVoiceLeft(v VoiceState) {
	cid := v.ChannelID
	v.ChannelID = 0
	s.toChannel(cid, "VOICE_STATE", map[string]any{"user_id": v.UserID, "guild_id": v.GuildID, "channel_id": nil, "left_channel_id": cid})
}

func (h *Hub) updateSelfVoice(uid store.ID, m clientMsg) {
	h.mu.Lock()
	v := h.voice[uid]
	if v == nil {
		h.mu.Unlock()
		return
	}
	set(&v.SelfMute, m.SelfMute)
	set(&v.SelfDeaf, m.SelfDeaf)
	set(&v.Streaming, m.Streaming)
	set(&v.Video, m.Video)
	cp := *v
	h.mu.Unlock()
	h.s.broadcastVoice(cp)
}

func set[T any](dst *T, v *T) {
	if v != nil {
		*dst = *v
	}
}

func (s *Server) livekitWebhook(w http.ResponseWriter, r *http.Request) {
	ev, err := s.media.ReceiveWebhook(r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "bad webhook")
		return
	}
	w.WriteHeader(http.StatusOK)
	if ev.Room == nil || ev.Participant == nil {
		return
	}
	cid, err1 := store.ParseID(ev.Room.Name)
	uid, err2 := store.ParseID(ev.Participant.Identity)
	if err1 != nil || err2 != nil {
		return
	}
	h := s.hub
	switch ev.Event {
	case "participant_joined":
		var gid store.ID
		var mute, deaf, isDM bool
		s.st.Read(func() {
			if c := s.st.Channel(cid); c != nil {
				gid = c.GuildID
				isDM = c.Type == "dm"
				if g := s.st.Guild(gid); g != nil {
					if m := g.Members[uid]; m != nil {
						mute, deaf = m.Mute, m.Deaf
					}
				}
			}
		})
		h.mu.Lock()
		p := h.pending[uid]
		delete(h.pending, uid)
		prev := h.voice[uid]
		v := &VoiceState{UserID: uid, GuildID: gid, ChannelID: cid, SelfMute: p.SelfMute, SelfDeaf: p.SelfDeaf, Mute: mute, Deaf: deaf, sid: ev.Participant.Sid}
		if prev != nil && prev.ChannelID == cid {
			// Same channel from a new device or a reconnect: keep the flags.
			v.SelfMute, v.SelfDeaf, v.Streaming, v.Video = prev.SelfMute, prev.SelfDeaf, prev.Streaming, prev.Video
		}
		h.voice[uid] = v
		cp := *v
		h.mu.Unlock()
		if prev != nil && prev.ChannelID != cid {
			s.broadcastVoiceLeft(*prev)
		}
		s.broadcastVoice(cp)
		if isDM && len(h.voiceIn(cid)) == 1 {
			var others []store.ID
			s.st.Read(func() {
				for _, id := range s.st.Channel(cid).Recipients {
					if id != uid {
						others = append(others, id)
					}
				}
			})
			h.toUsers(others, "CALL_RING", map[string]any{"channel_id": cid, "user_id": uid})
		}
	case "participant_left":
		h.mu.Lock()
		v := h.voice[uid]
		gone := v != nil && v.ChannelID == cid && v.sid == ev.Participant.Sid
		var cp VoiceState
		if gone {
			cp = *v
			delete(h.voice, uid)
		}
		h.mu.Unlock()
		if gone {
			s.broadcastVoiceLeft(cp)
		}
	case "track_published", "track_unpublished":
		if ev.Track == nil {
			return
		}
		on := ev.Event == "track_published"
		h.mu.Lock()
		v := h.voice[uid]
		if v == nil || v.ChannelID != cid {
			h.mu.Unlock()
			return
		}
		switch ev.Track.Source {
		case livekit.TrackSource_SCREEN_SHARE:
			v.Streaming = on
		case livekit.TrackSource_CAMERA:
			v.Video = on
		default:
			h.mu.Unlock()
			return
		}
		cp := *v
		h.mu.Unlock()
		s.broadcastVoice(cp)
	}
}
