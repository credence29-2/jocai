package store

import (
	"database/sql"
	"encoding/json"
	"strings"
)

type Attachment struct {
	ID          ID     `json:"id"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
}

type Reaction struct {
	Emoji string `json:"emoji"`
	Users []ID   `json:"users"`
}

type Message struct {
	ID              ID           `json:"id"`
	ChannelID       ID           `json:"channel_id"`
	GuildID         ID           `json:"guild_id"`
	Author          *User        `json:"author"`
	Type            string       `json:"type"`
	Content         string       `json:"content"`
	EditedAt        int64        `json:"edited_at"`
	ReplyTo         ID           `json:"reply_to"`
	Referenced      *Message     `json:"referenced,omitempty"`
	Attachments     []Attachment `json:"attachments"`
	Mentions        []ID         `json:"mentions"`
	MentionRoles    []ID         `json:"mention_roles"`
	MentionEveryone bool         `json:"mention_everyone"`
	Pinned          bool         `json:"pinned"`
	Reactions       []Reaction   `json:"reactions"`

	authorID ID
}

const msgCols = `id, channel_id, author_id, type, content, edited_at, reply_to, attachments, mentions, mention_roles, mention_everyone, pinned`

func scanMessage(sc interface{ Scan(...any) error }) (*Message, error) {
	m := &Message{}
	var att, men, mr string
	if err := sc.Scan(&m.ID, &m.ChannelID, &m.authorID, &m.Type, &m.Content, &m.EditedAt, &m.ReplyTo, &att, &men, &mr, &m.MentionEveryone, &m.Pinned); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(att), &m.Attachments)
	json.Unmarshal([]byte(men), &m.Mentions)
	json.Unmarshal([]byte(mr), &m.MentionRoles)
	return m, nil
}

// fill adds authors, replies and reactions.
func (s *Store) fill(msgs []*Message) error {
	if len(msgs) == 0 {
		return nil
	}
	ids := make([]any, len(msgs))
	byID := map[ID]*Message{}
	for i, m := range msgs {
		ids[i] = m.ID
		byID[m.ID] = m
		m.Reactions = []Reaction{}
		if m.Attachments == nil {
			m.Attachments = []Attachment{}
		}
		if m.Mentions == nil {
			m.Mentions = []ID{}
		}
		if m.MentionRoles == nil {
			m.MentionRoles = []ID{}
		}
	}
	q := `SELECT message_id, emoji, user_id FROM reactions WHERE message_id IN (?` + strings.Repeat(",?", len(ids)-1) + `) ORDER BY at`
	rows, err := s.db.Query(q, ids...)
	if err != nil {
		return err
	}
	for rows.Next() {
		var mid, uid ID
		var emoji string
		if err := rows.Scan(&mid, &emoji, &uid); err != nil {
			rows.Close()
			return err
		}
		m := byID[mid]
		found := false
		for i := range m.Reactions {
			if m.Reactions[i].Emoji == emoji {
				m.Reactions[i].Users = append(m.Reactions[i].Users, uid)
				found = true
			}
		}
		if !found {
			m.Reactions = append(m.Reactions, Reaction{Emoji: emoji, Users: []ID{uid}})
		}
	}
	rows.Close()
	for _, m := range msgs {
		if m.ReplyTo != 0 {
			r, err := scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, m.ReplyTo))
			if err == nil {
				r.Attachments, r.Mentions, r.MentionRoles, r.Reactions = []Attachment{}, []ID{}, []ID{}, []Reaction{}
				m.Referenced = r
			}
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range msgs {
		s.setAuthor(m)
		if m.Referenced != nil {
			s.setAuthor(m.Referenced)
		}
	}
	return nil
}

func (s *Store) setAuthor(m *Message) {
	m.Author = s.users[m.authorID]
	if m.Author == nil {
		m.Author = &User{ID: m.authorID, Username: "deleted-user", DisplayName: "Deleted User"}
	}
	if c := s.channels[m.ChannelID]; c != nil {
		m.GuildID = c.GuildID
	}
}

func (s *Store) CreateMessage(m *Message, authorID ID) (*Message, error) {
	m.ID = NewID()
	m.authorID = authorID
	if m.Type == "" {
		m.Type = "default"
	}
	if m.Attachments == nil {
		m.Attachments = []Attachment{}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO messages (id, channel_id, author_id, type, content, reply_to, attachments, mentions, mention_roles, mention_everyone) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.ChannelID, authorID, m.Type, m.Content, m.ReplyTo, jsonStr(m.Attachments), jsonStr(nonNil(m.Mentions)), jsonStr(nonNil(m.MentionRoles)), b2i(m.MentionEveryone)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE channels SET last_message_id = ? WHERE id = ?`, m.ID, m.ChannelID); err != nil {
		return nil, err
	}
	// Your own message counts as read.
	if _, err := tx.Exec(`INSERT OR REPLACE INTO read_states (user_id, channel_id, last_read_id) VALUES (?, ?, ?)`, authorID, m.ChannelID, m.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if c := s.channels[m.ChannelID]; c != nil {
		c.LastMessageID = m.ID
	}
	s.mu.Unlock()
	if err := s.fill([]*Message{m}); err != nil {
		return nil, err
	}
	return m, nil
}

func nonNil(ids []ID) []ID {
	if ids == nil {
		return []ID{}
	}
	return ids
}

func (s *Store) Message(id ID) (*Message, error) {
	m, err := scanMessage(s.db.QueryRow(`SELECT `+msgCols+` FROM messages WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return m, s.fill([]*Message{m})
}

// AuthorOf is cheaper than Message when only ownership matters.
func (m *Message) AuthorID() ID { return m.authorID }

// Messages pages through a channel. With before, it returns up to limit
// messages older than it; with after, newer; with neither, the latest.
// Results are oldest first.
func (s *Store) Messages(cid, before, after ID, limit int) ([]*Message, error) {
	var rows *sql.Rows
	var err error
	switch {
	case after != 0:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE channel_id = ? AND id > ? ORDER BY id LIMIT ?`, cid, after, limit)
	case before != 0:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE channel_id = ? AND id < ? ORDER BY id DESC LIMIT ?`, cid, before, limit)
	default:
		rows, err = s.db.Query(`SELECT `+msgCols+` FROM messages WHERE channel_id = ? ORDER BY id DESC LIMIT ?`, cid, limit)
	}
	if err != nil {
		return nil, err
	}
	out, err := collect(rows)
	if err != nil {
		return nil, err
	}
	if after == 0 {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, s.fill(out)
}

func collect(rows *sql.Rows) ([]*Message, error) {
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Pins(cid ID) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE channel_id = ? AND pinned = 1 ORDER BY id DESC LIMIT 50`, cid)
	if err != nil {
		return nil, err
	}
	out, err := collect(rows)
	if err != nil {
		return nil, err
	}
	return out, s.fill(out)
}

// Search finds messages containing every word of q in the given channels.
func (s *Store) Search(cids []ID, q string, authorID ID, limit int) ([]*Message, error) {
	if len(cids) == 0 {
		return []*Message{}, nil
	}
	args := []any{}
	where := `channel_id IN (?` + strings.Repeat(",?", len(cids)-1) + `)`
	for _, c := range cids {
		args = append(args, c)
	}
	for _, w := range strings.Fields(q) {
		where += ` AND content LIKE ? ESCAPE '\'`
		w = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(w)
		args = append(args, "%"+w+"%")
	}
	if authorID != 0 {
		where += ` AND author_id = ?`
		args = append(args, authorID)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT `+msgCols+` FROM messages WHERE `+where+` ORDER BY id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	out, err := collect(rows)
	if err != nil {
		return nil, err
	}
	return out, s.fill(out)
}

func (s *Store) EditMessage(id ID, content string, mentions, mentionRoles []ID, everyone bool) (*Message, error) {
	if _, err := s.db.Exec(`UPDATE messages SET content = ?, edited_at = ?, mentions = ?, mention_roles = ?, mention_everyone = ? WHERE id = ?`,
		content, now(), jsonStr(nonNil(mentions)), jsonStr(nonNil(mentionRoles)), b2i(everyone), id); err != nil {
		return nil, err
	}
	return s.Message(id)
}

func (s *Store) DeleteMessage(id ID) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return err
}

// AttachmentsIn lists every file posted in the given channels, so they can
// be removed from disk when the channels go.
func (s *Store) AttachmentsIn(cids []ID) []Attachment {
	var out []Attachment
	for _, cid := range cids {
		rows, err := s.db.Query(`SELECT attachments FROM messages WHERE channel_id = ? AND attachments != '[]'`, cid)
		if err != nil {
			continue
		}
		for rows.Next() {
			var js string
			var atts []Attachment
			if rows.Scan(&js) == nil && json.Unmarshal([]byte(js), &atts) == nil {
				out = append(out, atts...)
			}
		}
		rows.Close()
	}
	return out
}

func (s *Store) SetPinned(id ID, pinned bool) error {
	_, err := s.db.Exec(`UPDATE messages SET pinned = ? WHERE id = ?`, b2i(pinned), id)
	return err
}

func (s *Store) AddReaction(mid, uid ID, emoji string) (bool, error) {
	r, err := s.db.Exec(`INSERT OR IGNORE INTO reactions (message_id, user_id, emoji, at) VALUES (?, ?, ?, ?)`, mid, uid, emoji, now())
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

func (s *Store) RemoveReaction(mid, uid ID, emoji string) (bool, error) {
	r, err := s.db.Exec(`DELETE FROM reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`, mid, uid, emoji)
	if err != nil {
		return false, err
	}
	n, _ := r.RowsAffected()
	return n > 0, nil
}

func (s *Store) Ack(uid, cid, mid ID) error {
	_, err := s.db.Exec(`INSERT INTO read_states (user_id, channel_id, last_read_id) VALUES (?, ?, ?)
		ON CONFLICT (user_id, channel_id) DO UPDATE SET last_read_id = MAX(last_read_id, excluded.last_read_id)`, uid, cid, mid)
	return err
}

type ReadState struct {
	ChannelID    ID  `json:"channel_id"`
	LastReadID   ID  `json:"last_read_id"`
	MentionCount int `json:"mention_count"`
}

// ReadStates returns where a user left off in each channel, with how many
// unread messages since then mention them. roleIDs is every role the user
// holds anywhere, for role mentions.
func (s *Store) ReadStates(uid ID, roleIDs map[ID]bool, visible map[ID]bool) ([]ReadState, error) {
	rows, err := s.db.Query(`SELECT channel_id, last_read_id FROM read_states WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	states := map[ID]*ReadState{}
	for rows.Next() {
		rs := &ReadState{}
		if err := rows.Scan(&rs.ChannelID, &rs.LastReadID); err != nil {
			rows.Close()
			return nil, err
		}
		states[rs.ChannelID] = rs
	}
	rows.Close()
	out := []ReadState{}
	for cid := range visible {
		rs := states[cid]
		if rs == nil {
			rs = &ReadState{ChannelID: cid}
		}
		s.mu.RLock()
		c := s.channels[cid]
		unread := c != nil && c.LastMessageID > rs.LastReadID
		isDM := c != nil && c.Type == "dm"
		s.mu.RUnlock()
		if unread {
			mrows, err := s.db.Query(`SELECT author_id, mentions, mention_roles, mention_everyone FROM messages WHERE channel_id = ? AND id > ? ORDER BY id DESC LIMIT 100`, cid, rs.LastReadID)
			if err != nil {
				return nil, err
			}
			for mrows.Next() {
				var author ID
				var men, mr string
				var all bool
				mrows.Scan(&author, &men, &mr, &all)
				if author == uid {
					continue
				}
				if isDM || all || strings.Contains(men, `"`+uid.String()+`"`) || mentionsRole(mr, roleIDs) {
					rs.MentionCount++
				}
			}
			mrows.Close()
		}
		out = append(out, *rs)
	}
	return out, nil
}

func mentionsRole(js string, roles map[ID]bool) bool {
	var ids []ID
	json.Unmarshal([]byte(js), &ids)
	for _, id := range ids {
		if roles[id] {
			return true
		}
	}
	return false
}
