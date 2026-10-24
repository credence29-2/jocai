// Package store keeps Victor Charlie's data in SQLite and mirrors the parts every
// request needs (users, servers, channels, roles, members) in memory so
// permission checks never touch the disk.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

// ID is a time-ordered 63-bit id. It is sent to clients as a string because
// JavaScript numbers can't hold it exactly.
type ID int64

const epoch = 1767225600000 // 2026-01-01 UTC, in ms

var (
	idMu   sync.Mutex
	idLast int64
	idSeq  int64
)

func NewID() ID {
	idMu.Lock()
	defer idMu.Unlock()
	ms := time.Now().UnixMilli() - epoch
	if ms <= idLast {
		ms = idLast
		idSeq++
		if idSeq >= 1<<22 {
			ms++
			idSeq = 0
		}
	} else {
		idSeq = 0
	}
	idLast = ms
	return ID(ms<<22 | idSeq)
}

func (id ID) String() string { return strconv.FormatInt(int64(id), 10) }

func (id ID) MarshalJSON() ([]byte, error) {
	if id == 0 {
		return []byte("null"), nil
	}
	return []byte(`"` + id.String() + `"`), nil
}

func (id *ID) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` {
		*id = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		s = s[1 : len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("bad id %q", s)
	}
	*id = ID(n)
	return nil
}

func ParseID(s string) (ID, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, ErrNotFound
	}
	return ID(n), nil
}

// Time returns when the id was made.
func (id ID) Time() time.Time { return time.UnixMilli(int64(id)>>22 + epoch) }

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id INTEGER PRIMARY KEY,
	username TEXT NOT NULL UNIQUE COLLATE NOCASE,
	display_name TEXT NOT NULL DEFAULT '',
	password_hash TEXT NOT NULL,
	avatar TEXT NOT NULL DEFAULT '',
	about TEXT NOT NULL DEFAULT '',
	accent INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'online',
	custom_status TEXT NOT NULL DEFAULT '',
	is_admin INTEGER NOT NULL DEFAULT 0,
	disabled INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS sessions (
	token_hash TEXT PRIMARY KEY,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	last_seen INTEGER NOT NULL,
	agent TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS guilds (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL,
	icon TEXT NOT NULL DEFAULT '',
	owner_id INTEGER NOT NULL,
	afk_channel_id INTEGER NOT NULL DEFAULT 0,
	system_channel_id INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS roles (
	id INTEGER PRIMARY KEY,
	guild_id INTEGER NOT NULL REFERENCES guilds(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	color INTEGER NOT NULL DEFAULT 0,
	position INTEGER NOT NULL DEFAULT 0,
	permissions INTEGER NOT NULL DEFAULT 0,
	hoist INTEGER NOT NULL DEFAULT 0,
	mentionable INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS channels (
	id INTEGER PRIMARY KEY,
	guild_id INTEGER NOT NULL DEFAULT 0,
	type TEXT NOT NULL,
	name TEXT NOT NULL DEFAULT '',
	topic TEXT NOT NULL DEFAULT '',
	position INTEGER NOT NULL DEFAULT 0,
	parent_id INTEGER NOT NULL DEFAULT 0,
	overwrites TEXT NOT NULL DEFAULT '[]',
	bitrate INTEGER NOT NULL DEFAULT 96,
	user_limit INTEGER NOT NULL DEFAULT 0,
	slowmode INTEGER NOT NULL DEFAULT 0,
	owner_id INTEGER NOT NULL DEFAULT 0,
	last_message_id INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS dm_recipients (
	channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL,
	PRIMARY KEY (channel_id, user_id)
);
CREATE TABLE IF NOT EXISTS members (
	guild_id INTEGER NOT NULL REFERENCES guilds(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	nick TEXT NOT NULL DEFAULT '',
	roles TEXT NOT NULL DEFAULT '[]',
	joined_at INTEGER NOT NULL,
	mute INTEGER NOT NULL DEFAULT 0,
	deaf INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (guild_id, user_id)
);
CREATE TABLE IF NOT EXISTS bans (
	guild_id INTEGER NOT NULL REFERENCES guilds(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL,
	reason TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (guild_id, user_id)
);
CREATE TABLE IF NOT EXISTS invites (
	code TEXT PRIMARY KEY,
	guild_id INTEGER NOT NULL REFERENCES guilds(id) ON DELETE CASCADE,
	creator_id INTEGER NOT NULL,
	uses INTEGER NOT NULL DEFAULT 0,
	max_uses INTEGER NOT NULL DEFAULT 0,
	expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
	id INTEGER PRIMARY KEY,
	channel_id INTEGER NOT NULL,
	author_id INTEGER NOT NULL,
	type TEXT NOT NULL DEFAULT 'default',
	content TEXT NOT NULL DEFAULT '',
	edited_at INTEGER NOT NULL DEFAULT 0,
	reply_to INTEGER NOT NULL DEFAULT 0,
	attachments TEXT NOT NULL DEFAULT '[]',
	mentions TEXT NOT NULL DEFAULT '[]',
	mention_roles TEXT NOT NULL DEFAULT '[]',
	mention_everyone INTEGER NOT NULL DEFAULT 0,
	pinned INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS messages_channel ON messages(channel_id, id);
CREATE TABLE IF NOT EXISTS reactions (
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	user_id INTEGER NOT NULL,
	emoji TEXT NOT NULL,
	at INTEGER NOT NULL,
	PRIMARY KEY (message_id, user_id, emoji)
);
CREATE TABLE IF NOT EXISTS read_states (
	user_id INTEGER NOT NULL,
	channel_id INTEGER NOT NULL,
	last_read_id INTEGER NOT NULL,
	PRIMARY KEY (user_id, channel_id)
);
`

type Store struct {
	db      *sql.DB
	DataDir string

	mu       sync.RWMutex
	users    map[ID]*User
	byName   map[string]ID
	guilds   map[ID]*Guild
	channels map[ID]*Channel // guild channels and DMs
	dms      map[ID][]ID     // user -> dm channel ids
}

func Open(dataDir string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dataDir, "victorcharlie.db")+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, err
	}
	// One connection keeps SQLite writes serialized without SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	s := &Store{db: db, DataDir: dataDir}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func now() int64 { return time.Now().UnixMilli() }

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
