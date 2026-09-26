package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type User struct {
	ID              string   `json:"id"`
	FullName        string   `json:"full_name"`
	Email           string   `json:"email"`
	PasswordHash    string   `json:"-"`
	OllamaBaseURL   *string  `json:"ollama_base_url,omitempty"`
	PreferredModels []string `json:"preferred_models"`
	BraveAPIKey     *string  `json:"-"`
	NumCtx          *int     `json:"num_ctx,omitempty"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

func decodePreferredModels(raw *string) []string {
	if raw == nil || *raw == "" {
		return []string{}
	}
	var models []string
	if err := json.Unmarshal([]byte(*raw), &models); err != nil {
		return []string{}
	}
	return models
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt int64
}

type SecurityQuestion struct {
	ID         string `json:"id"`
	UserID     string `json:"-"`
	Question   string `json:"question"`
	AnswerHash string `json:"-"`
	CreatedAt  int64  `json:"created_at"`
}

type Conversation struct {
	ID             string  `json:"id"`
	UserID         string  `json:"-"`
	Title          string  `json:"title"`
	Model          string  `json:"model"`
	AttachedFolder *string `json:"attached_folder,omitempty"`
	ContextUsed    int     `json:"context_used"`
	ContextMax     int     `json:"context_max"`
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

type Message struct {
	ID           int64        `json:"id"`
	Role         string       `json:"role"`
	Content      string       `json:"content"`
	ToolCalls    *string      `json:"tool_calls,omitempty"`
	ToolCallID   *string      `json:"tool_call_id,omitempty"`
	TokensPerSec *float64     `json:"tokens_per_sec,omitempty"`
	Thinking     string       `json:"thinking,omitempty"`
	Attachments  []Attachment `json:"attachments,omitempty"`
	CreatedAt    int64        `json:"created_at,omitempty"`
}

type Attachment struct {
	ID        string `json:"id"`
	MessageID int64  `json:"-"`
	MimeType  string `json:"mime_type"`
	Filename  string `json:"filename"`
	FilePath  string `json:"-"`
	CreatedAt int64  `json:"created_at"`
}

type ConversationWithMessages struct {
	Conversation
	Messages []Message `json:"messages"`
	// Summaries not yet folded into a higher level, oldest first.
	Summaries []Summary `json:"-"`
	// Real prompt tokens per estimated token, measured on the last request.
	TokenRatio float64 `json:"-"`
}

// Summary stands in for messages FirstMessageID..LastMessageID when
// building history. Level 0 condenses messages; level n+1 condenses level-n
// summaries, which are then marked merged but kept.
type Summary struct {
	ID             int64
	Level          int
	FirstMessageID int64
	LastMessageID  int64
	Content        string
	UserNotes      string
}

type Command struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"-"`
	ToolCallID     string  `json:"-"`
	Command        string  `json:"command"`
	Cwd            string  `json:"cwd"`
	Status         string  `json:"status"`
	Output         *string `json:"output,omitempty"`
	ExitCode       *int    `json:"exit_code,omitempty"`
	CreatedAt      int64   `json:"created_at"`
	DecidedAt      *int64  `json:"decided_at,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id               TEXT PRIMARY KEY,
	full_name        TEXT NOT NULL,
	email            TEXT NOT NULL UNIQUE,
	password_hash    TEXT NOT NULL,
	ollama_base_url  TEXT,
	preferred_models TEXT,
	brave_api_key    TEXT,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS security_questions (
	id          TEXT PRIMARY KEY,
	user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	question    TEXT NOT NULL,
	answer_hash TEXT NOT NULL,
	created_at  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_security_questions_user_id ON security_questions(user_id);

CREATE TABLE IF NOT EXISTS conversations (
	id              TEXT PRIMARY KEY,
	user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title           TEXT NOT NULL,
	model           TEXT NOT NULL,
	attached_folder TEXT,
	context_used    INTEGER NOT NULL DEFAULT 0,
	context_max     INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	tool_calls      TEXT,
	tool_call_id    TEXT,
	tokens_per_sec  REAL,
	thinking        TEXT NOT NULL DEFAULT '',
	created_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS commands (
	id              TEXT PRIMARY KEY,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	tool_call_id    TEXT NOT NULL,
	command         TEXT NOT NULL,
	cwd             TEXT NOT NULL,
	status          TEXT NOT NULL,
	output          TEXT,
	exit_code       INTEGER,
	created_at      INTEGER NOT NULL,
	decided_at      INTEGER
);

CREATE TABLE IF NOT EXISTS attachments (
	id         TEXT PRIMARY KEY,
	message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
	mime_type  TEXT NOT NULL,
	filename   TEXT NOT NULL DEFAULT '',
	file_path  TEXT NOT NULL,
	created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS summaries (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id  TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	level            INTEGER NOT NULL,
	first_message_id INTEGER NOT NULL,
	last_message_id  INTEGER NOT NULL,
	content          TEXT NOT NULL,
	user_notes       TEXT NOT NULL DEFAULT '',
	merged           INTEGER NOT NULL DEFAULT 0,
	created_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_summaries_conversation_id ON summaries(conversation_id);

CREATE TABLE IF NOT EXISTS memories (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	folder     TEXT,
	content    TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_memories_user_id ON memories(user_id);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(content, content='memories', content_rowid='id');
CREATE TRIGGER IF NOT EXISTS memories_fts_insert AFTER INSERT ON memories BEGIN
	INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER IF NOT EXISTS memories_fts_delete AFTER DELETE ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER IF NOT EXISTS memories_fts_update AFTER UPDATE OF content ON memories BEGIN
	INSERT INTO memories_fts(memories_fts, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO memories_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON conversations(user_id);
CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_commands_conversation_id ON commands(conversation_id);
CREATE INDEX IF NOT EXISTS idx_attachments_message_id ON attachments(message_id);
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return db, nil
}

// migrate covers changes CREATE TABLE IF NOT EXISTS can't retrofit onto a
// database that already existed before the change - new columns on an
// existing table. Each statement is idempotent (ignores "duplicate column"
// so re-running against an already-migrated database is a no-op.
func migrate(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE attachments ADD COLUMN filename TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE messages ADD COLUMN tokens_per_sec REAL`,
		`ALTER TABLE messages ADD COLUMN thinking TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE conversations ADD COLUMN context_used INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE conversations ADD COLUMN context_max INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE conversations ADD COLUMN token_ratio REAL NOT NULL DEFAULT 1`,
		// The chat a memory was saved from, so that chat can show where it
		// happened. A memory outlives its chat, hence SET NULL.
		`ALTER TABLE memories ADD COLUMN conversation_id TEXT REFERENCES conversations(id) ON DELETE SET NULL`,
		`ALTER TABLE users ADD COLUMN num_ctx INTEGER`,
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return migrateSearchIndex(db)
}

// An external-content FTS5 index over message text, kept in step by
// triggers. Created here rather than in schema so a database from before
// search existed gets its old messages indexed exactly once.
func migrateSearchIndex(db *sql.DB) error {
	var exists int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'messages_fts'`).Scan(&exists); err != nil || exists > 0 {
		return err
	}
	_, err := db.Exec(`
CREATE VIRTUAL TABLE messages_fts USING fts5(content, content='messages', content_rowid='id');
CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
CREATE TRIGGER messages_fts_delete AFTER DELETE ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
END;
CREATE TRIGGER messages_fts_update AFTER UPDATE OF content ON messages BEGIN
	INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.id, old.content);
	INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
INSERT INTO messages_fts(messages_fts) VALUES ('rebuild');`)
	return err
}

type ChatSearchResult struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
}

// ftsQuery turns free text into a safe FTS5 query: every word is quoted (so
// punctuation is never parsed as FTS syntax) and prefix-matched, so results
// appear while a word is still being typed.
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.Fields(q) {
		terms = append(terms, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
	}
	return strings.Join(terms, " ")
}

const maxSearchResults = 20

// Only user and assistant text is searched: tool output and system messages
// (folder manifests, web results) would match nearly any query and bury
// the conversation the user is actually looking for.
func searchConversations(db *sql.DB, userID, q string) ([]ChatSearchResult, error) {
	match := ftsQuery(q)
	out := []ChatSearchResult{}
	if match == "" {
		return out, nil
	}
	rows, err := db.Query(`
SELECT id, title, '' FROM conversations
WHERE user_id = ? AND title LIKE '%' || ? || '%' ESCAPE '\'
UNION ALL
SELECT * FROM (
	SELECT c.id, c.title, snippet(messages_fts, 0, '', '', '…', 12)
	FROM messages_fts
	JOIN messages m ON m.id = messages_fts.rowid
	JOIN conversations c ON c.id = m.conversation_id
	WHERE messages_fts MATCH ? AND c.user_id = ? AND m.role IN ('user', 'assistant')
	ORDER BY rank
	LIMIT 200
)`, userID, likeEscaper.Replace(strings.TrimSpace(q)), match, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var r ChatSearchResult
		if err := rows.Scan(&r.ID, &r.Title, &r.Snippet); err != nil {
			return nil, err
		}
		if seen[r.ID] || len(out) >= maxSearchResults {
			continue
		}
		seen[r.ID] = true
		out = append(out, r)
	}
	return out, rows.Err()
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func createUser(db *sql.DB, id, fullName, email, passwordHash string) (User, error) {
	now := time.Now().UnixMilli()
	u := User{ID: id, FullName: fullName, Email: email, PasswordHash: passwordHash, PreferredModels: []string{}, CreatedAt: now, UpdatedAt: now}
	_, err := db.Exec(
		`INSERT INTO users (id, full_name, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.FullName, u.Email, u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	)
	return u, err
}

func getUserByEmail(db *sql.DB, email string) (*User, error) {
	var u User
	var preferredModelsRaw *string
	err := db.QueryRow(
		`SELECT id, full_name, email, password_hash, ollama_base_url, preferred_models, brave_api_key, num_ctx, created_at, updated_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.NumCtx, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.PreferredModels = decodePreferredModels(preferredModelsRaw)
	return &u, nil
}

func getUserByID(db *sql.DB, id string) (*User, error) {
	var u User
	var preferredModelsRaw *string
	err := db.QueryRow(
		`SELECT id, full_name, email, password_hash, ollama_base_url, preferred_models, brave_api_key, num_ctx, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.NumCtx, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.PreferredModels = decodePreferredModels(preferredModelsRaw)
	return &u, nil
}

func updateUserOllamaURL(db *sql.DB, userID string, baseURL *string) error {
	_, err := db.Exec(
		`UPDATE users SET ollama_base_url = ?, updated_at = ? WHERE id = ?`,
		baseURL, time.Now().UnixMilli(), userID,
	)
	return err
}

func updateUserNumCtx(db *sql.DB, userID string, numCtx *int) error {
	_, err := db.Exec(`UPDATE users SET num_ctx = ?, updated_at = ? WHERE id = ?`, numCtx, time.Now().UnixMilli(), userID)
	return err
}

func updateUserPreferredModels(db *sql.DB, userID string, models []string) error {
	data, err := json.Marshal(models)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`UPDATE users SET preferred_models = ?, updated_at = ? WHERE id = ?`,
		string(data), time.Now().UnixMilli(), userID,
	)
	return err
}

func updateUserBraveAPIKey(db *sql.DB, userID string, key *string) error {
	_, err := db.Exec(
		`UPDATE users SET brave_api_key = ?, updated_at = ? WHERE id = ?`,
		key, time.Now().UnixMilli(), userID,
	)
	return err
}

func updateUserProfile(db *sql.DB, userID, fullName, email string) error {
	_, err := db.Exec(
		`UPDATE users SET full_name = ?, email = ?, updated_at = ? WHERE id = ?`,
		fullName, email, time.Now().UnixMilli(), userID,
	)
	return err
}

func updateUserPassword(db *sql.DB, userID, passwordHash string) error {
	_, err := db.Exec(
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, time.Now().UnixMilli(), userID,
	)
	return err
}

func createSession(db *sql.DB, id, userID string, ttl time.Duration) (Session, error) {
	now := time.Now()
	s := Session{ID: id, UserID: userID, ExpiresAt: now.Add(ttl).UnixMilli()}
	_, err := db.Exec(
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		s.ID, s.UserID, now.UnixMilli(), s.ExpiresAt,
	)
	return s, err
}

func getSessionUser(db *sql.DB, sessionID string) (*User, error) {
	var u User
	var preferredModelsRaw *string
	var expiresAt int64
	err := db.QueryRow(
		`SELECT u.id, u.full_name, u.email, u.password_hash, u.ollama_base_url, u.preferred_models, u.brave_api_key, u.num_ctx, u.created_at, u.updated_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?`, sessionID,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.NumCtx, &u.CreatedAt, &u.UpdatedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt < time.Now().UnixMilli() {
		return nil, nil
	}
	u.PreferredModels = decodePreferredModels(preferredModelsRaw)
	return &u, nil
}

func deleteSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

func deleteOtherSessions(db *sql.DB, userID, keepSessionID string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepSessionID)
	return err
}

func deleteAllSessionsForUser(db *sql.DB, userID string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func setSecurityQuestions(db *sql.DB, userID string, questions []SecurityQuestion) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM security_questions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, q := range questions {
		if _, err := tx.Exec(
			`INSERT INTO security_questions (id, user_id, question, answer_hash, created_at) VALUES (?, ?, ?, ?, ?)`,
			q.ID, userID, q.Question, q.AnswerHash, time.Now().UnixMilli(),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func getSecurityQuestionsForUser(db *sql.DB, userID string) ([]SecurityQuestion, error) {
	rows, err := db.Query(
		`SELECT id, user_id, question, answer_hash, created_at FROM security_questions WHERE user_id = ? ORDER BY created_at ASC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SecurityQuestion{}
	for rows.Next() {
		var q SecurityQuestion
		if err := rows.Scan(&q.ID, &q.UserID, &q.Question, &q.AnswerHash, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

func getSecurityQuestionsByEmail(db *sql.DB, email string) (userID string, questions []SecurityQuestion, err error) {
	user, err := getUserByEmail(db, email)
	if err != nil {
		return "", nil, err
	}
	if user == nil {
		return "", nil, nil
	}
	questions, err = getSecurityQuestionsForUser(db, user.ID)
	if err != nil {
		return "", nil, err
	}
	return user.ID, questions, nil
}

func createConversation(db *sql.DB, id, userID, model string) (Conversation, error) {
	now := time.Now().UnixMilli()
	c := Conversation{ID: id, UserID: userID, Title: "New chat", Model: model, CreatedAt: now, UpdatedAt: now}
	_, err := db.Exec(
		`INSERT INTO conversations (id, user_id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.ID, c.UserID, c.Title, c.Model, c.CreatedAt, c.UpdatedAt,
	)
	return c, err
}

func listConversations(db *sql.DB, userID string) ([]Conversation, error) {
	rows, err := db.Query(
		`SELECT id, user_id, title, model, attached_folder, created_at, updated_at FROM conversations WHERE user_id = ? ORDER BY updated_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.AttachedFolder, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func getConversation(db *sql.DB, id, userID string) (*ConversationWithMessages, error) {
	var c Conversation
	var ratio float64
	err := db.QueryRow(
		`SELECT id, user_id, title, model, attached_folder, context_used, context_max, token_ratio, created_at, updated_at FROM conversations WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.AttachedFolder, &c.ContextUsed, &c.ContextMax, &ratio, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT id, role, content, tool_calls, tool_call_id, tokens_per_sec, thinking, created_at FROM messages WHERE conversation_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID, &m.TokensPerSec, &m.Thinking, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	attachRows, err := db.Query(
		`SELECT a.id, a.message_id, a.mime_type, a.filename, a.file_path, a.created_at
		 FROM attachments a JOIN messages m ON m.id = a.message_id
		 WHERE m.conversation_id = ? ORDER BY a.created_at ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer attachRows.Close()

	byMessage := map[int64][]Attachment{}
	for attachRows.Next() {
		var a Attachment
		if err := attachRows.Scan(&a.ID, &a.MessageID, &a.MimeType, &a.Filename, &a.FilePath, &a.CreatedAt); err != nil {
			return nil, err
		}
		byMessage[a.MessageID] = append(byMessage[a.MessageID], a)
	}
	if err := attachRows.Err(); err != nil {
		return nil, err
	}
	for i := range messages {
		messages[i].Attachments = byMessage[messages[i].ID]
	}

	summaries, err := activeSummaries(db, id)
	if err != nil {
		return nil, err
	}
	return &ConversationWithMessages{Conversation: c, Messages: messages, Summaries: summaries, TokenRatio: ratio}, nil
}

func activeSummaries(db *sql.DB, conversationID string) ([]Summary, error) {
	rows, err := db.Query(
		`SELECT id, level, first_message_id, last_message_id, content, user_notes FROM summaries WHERE conversation_id = ? AND merged = 0 ORDER BY first_message_id ASC`, conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Level, &s.FirstMessageID, &s.LastMessageID, &s.Content, &s.UserNotes); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// saveSummary stores a new summary and marks the summaries it replaces as
// merged, in one transaction so history never sees both or neither.
func saveSummary(db *sql.DB, conversationID string, s Summary, replaces []Summary) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO summaries (conversation_id, level, first_message_id, last_message_id, content, user_notes, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		conversationID, s.Level, s.FirstMessageID, s.LastMessageID, s.Content, s.UserNotes, time.Now().UnixMilli(),
	); err != nil {
		return err
	}
	for _, r := range replaces {
		if _, err := tx.Exec(`UPDATE summaries SET merged = 1 WHERE id = ?`, r.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func setTokenRatio(db *sql.DB, id string, ratio float64) error {
	_, err := db.Exec(`UPDATE conversations SET token_ratio = ? WHERE id = ?`, ratio, id)
	return err
}

func updateMessageContent(db *sql.DB, id int64, content string) error {
	_, err := db.Exec(`UPDATE messages SET content = ? WHERE id = ?`, content, id)
	return err
}

func setAttachedFolder(db *sql.DB, id, folder string) error {
	_, err := db.Exec(`UPDATE conversations SET attached_folder = ?, updated_at = ? WHERE id = ?`, folder, time.Now().UnixMilli(), id)
	return err
}

func deleteConversation(db *sql.DB, id, userID string) (bool, error) {
	res, err := db.Exec(`DELETE FROM conversations WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func insertMessage(db *sql.DB, conversationID, role, content string) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		conversationID, role, content, time.Now().UnixMilli(),
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func insertAssistantMessage(db *sql.DB, conversationID, content, thinking string, tokensPerSec float64) error {
	var tps *float64
	if tokensPerSec > 0 {
		tps = &tokensPerSec
	}
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, thinking, tokens_per_sec, created_at) VALUES (?, 'assistant', ?, ?, ?, ?)`,
		conversationID, content, thinking, tps, time.Now().UnixMilli(),
	)
	return err
}

// Drops messages from fromID onward and every shell command proposed since
// fromTime, including a still-pending one. Attachment rows cascade with
// their message, but the files stay on disk so an edited message can
// re-link them.
func truncateConversation(db *sql.DB, conversationID string, fromID, fromTime int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM commands WHERE conversation_id = ? AND created_at >= ?`, conversationID, fromTime); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE conversation_id = ? AND id >= ?`, conversationID, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM summaries WHERE conversation_id = ? AND last_message_id >= ?`, conversationID, fromID); err != nil {
		return err
	}
	return tx.Commit()
}

func createAttachment(db *sql.DB, id string, messageID int64, mimeType, filename, filePath string) error {
	_, err := db.Exec(
		`INSERT INTO attachments (id, message_id, mime_type, filename, file_path, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, messageID, mimeType, filename, filePath, time.Now().UnixMilli(),
	)
	return err
}

func getAttachmentPathsForConversation(db *sql.DB, conversationID string) ([]string, error) {
	return queryStrings(db, `SELECT a.file_path FROM attachments a JOIN messages m ON m.id = a.message_id WHERE m.conversation_id = ?`, conversationID)
}

func getAttachmentPathsForUser(db *sql.DB, userID string) ([]string, error) {
	return queryStrings(db, `SELECT a.file_path FROM attachments a JOIN messages m ON m.id = a.message_id
		JOIN conversations c ON c.id = m.conversation_id WHERE c.user_id = ?`, userID)
}

// deleteUser removes the account; every table cascades from users.
func deleteUser(db *sql.DB, userID string) error {
	_, err := db.Exec(`DELETE FROM users WHERE id = ?`, userID)
	return err
}

func queryStrings(db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func getAttachmentOwned(db *sql.DB, attachmentID, userID string) (*Attachment, error) {
	var a Attachment
	err := db.QueryRow(
		`SELECT a.id, a.message_id, a.mime_type, a.filename, a.file_path, a.created_at
		 FROM attachments a
		 JOIN messages m ON m.id = a.message_id
		 JOIN conversations c ON c.id = m.conversation_id
		 WHERE a.id = ? AND c.user_id = ?`, attachmentID, userID,
	).Scan(&a.ID, &a.MessageID, &a.MimeType, &a.Filename, &a.FilePath, &a.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

func insertToolCallMessage(db *sql.DB, conversationID, content, thinking, toolCallsJSON string) error {
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, thinking, tool_calls, created_at) VALUES (?, 'assistant', ?, ?, ?, ?)`,
		conversationID, content, thinking, toolCallsJSON, time.Now().UnixMilli(),
	)
	return err
}

func insertToolResultMessage(db *sql.DB, conversationID, toolCallID, content string) error {
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, tool_call_id, created_at) VALUES (?, 'tool', ?, ?, ?)`,
		conversationID, content, toolCallID, time.Now().UnixMilli(),
	)
	return err
}

func setContextUsage(db *sql.DB, id string, used, max int) error {
	_, err := db.Exec(`UPDATE conversations SET context_used = ?, context_max = ? WHERE id = ?`, used, max, id)
	return err
}

func touchConversation(db *sql.DB, id string) error {
	_, err := db.Exec(`UPDATE conversations SET updated_at = ? WHERE id = ?`, time.Now().UnixMilli(), id)
	return err
}

const maxTitleRunes = 60

// Truncates by rune, not byte, so a multibyte character is never split
// into invalid UTF-8; newlines are collapsed so a title stays one line.
func normalizeTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > maxTitleRunes {
		s = string(r[:maxTitleRunes])
	}
	return s
}

// Sets an immediate placeholder title from the first message and reports
// whether it did, i.e. whether this was the conversation's first message.
func maybeSetTitle(db *sql.DB, id, firstMessage string) (string, bool, error) {
	title := normalizeTitle(firstMessage)
	if title == "" {
		return "", false, nil
	}
	res, err := db.Exec(`UPDATE conversations SET title = ? WHERE id = ? AND title = 'New chat'`, title, id)
	if err != nil {
		return "", false, err
	}
	n, err := res.RowsAffected()
	return title, n > 0, err
}

// Only replaces the title if it is still the placeholder, so a rename the
// user made in the meantime always wins.
func replacePlaceholderTitle(db *sql.DB, id, placeholder, title string) error {
	_, err := db.Exec(`UPDATE conversations SET title = ? WHERE id = ? AND title = ?`, title, id, placeholder)
	return err
}

func renameConversation(db *sql.DB, id, userID, title string) (bool, error) {
	res, err := db.Exec(`UPDATE conversations SET title = ? WHERE id = ? AND user_id = ?`, title, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func createCommand(db *sql.DB, id, conversationID, toolCallID, command, cwd string) (Command, error) {
	now := time.Now().UnixMilli()
	c := Command{ID: id, ConversationID: conversationID, ToolCallID: toolCallID, Command: command, Cwd: cwd, Status: "pending", CreatedAt: now}
	_, err := db.Exec(
		`INSERT INTO commands (id, conversation_id, tool_call_id, command, cwd, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.ConversationID, c.ToolCallID, c.Command, c.Cwd, c.Status, c.CreatedAt,
	)
	return c, err
}

func getCommand(db *sql.DB, id, conversationID string) (*Command, error) {
	var c Command
	err := db.QueryRow(
		`SELECT id, conversation_id, tool_call_id, command, cwd, status, output, exit_code, created_at, decided_at
		 FROM commands WHERE id = ? AND conversation_id = ?`, id, conversationID,
	).Scan(&c.ID, &c.ConversationID, &c.ToolCallID, &c.Command, &c.Cwd, &c.Status, &c.Output, &c.ExitCode, &c.CreatedAt, &c.DecidedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func getPendingCommand(db *sql.DB, conversationID string) (*Command, error) {
	var c Command
	err := db.QueryRow(
		`SELECT id, conversation_id, tool_call_id, command, cwd, status, output, exit_code, created_at, decided_at
		 FROM commands WHERE conversation_id = ? AND status = 'pending' ORDER BY created_at DESC LIMIT 1`, conversationID,
	).Scan(&c.ID, &c.ConversationID, &c.ToolCallID, &c.Command, &c.Cwd, &c.Status, &c.Output, &c.ExitCode, &c.CreatedAt, &c.DecidedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func resolveCommand(db *sql.DB, id, status, output string, exitCode *int) error {
	_, err := db.Exec(
		`UPDATE commands SET status = ?, output = ?, exit_code = ?, decided_at = ? WHERE id = ?`,
		status, output, exitCode, time.Now().UnixMilli(), id,
	)
	return err
}

type Memory struct {
	ID        int64   `json:"id"`
	Folder    *string `json:"folder,omitempty"`
	Content   string  `json:"content"`
	CreatedAt int64   `json:"created_at"`
	UpdatedAt int64   `json:"updated_at"`
	// Only set by listMemories, for the Settings "From: <chat>" link.
	SourceID    *string `json:"source_id,omitempty"`
	SourceTitle *string `json:"source_title,omitempty"`
}

func scanMemories(rows *sql.Rows) ([]Memory, error) {
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Folder, &m.Content, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func createMemory(db *sql.DB, userID string, folder, conversationID *string, content string) (Memory, error) {
	now := time.Now().UnixMilli()
	m := Memory{Folder: folder, Content: content, CreatedAt: now, UpdatedAt: now}
	res, err := db.Exec(`INSERT INTO memories (user_id, folder, conversation_id, content, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, m.Folder, conversationID, m.Content, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return m, err
	}
	m.ID, err = res.LastInsertId()
	return m, err
}

// chatMemories are the memories saved from one conversation, oldest first.
func chatMemories(db *sql.DB, userID, conversationID string) ([]Memory, error) {
	rows, err := db.Query(`SELECT id, folder, content, created_at, updated_at FROM memories WHERE user_id = ? AND conversation_id = ? ORDER BY created_at`, userID, conversationID)
	if err != nil {
		return nil, err
	}
	return scanMemories(rows)
}

// listMemories joins the source chat on the owner too, so a memory can
// only ever name one of the caller's own chats.
func listMemories(db *sql.DB, userID string) ([]Memory, error) {
	rows, err := db.Query(`
SELECT m.id, m.folder, m.content, m.created_at, m.updated_at, c.id, c.title
FROM memories m LEFT JOIN conversations c ON c.id = m.conversation_id AND c.user_id = m.user_id
WHERE m.user_id = ? ORDER BY m.updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Memory{}
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Folder, &m.Content, &m.CreatedAt, &m.UpdatedAt, &m.SourceID, &m.SourceTitle); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func folderMemories(db *sql.DB, userID, folder string) ([]Memory, error) {
	rows, err := db.Query(`SELECT id, folder, content, created_at, updated_at FROM memories WHERE user_id = ? AND folder = ? ORDER BY updated_at DESC`, userID, folder)
	if err != nil {
		return nil, err
	}
	return scanMemories(rows)
}

// searchMemories ranks by any matching word, not all of them: a recall
// like "@memory astra deadline" should find a memory that only mentions
// astra.
func searchMemories(db *sql.DB, userID, q string) ([]Memory, error) {
	var terms []string
	for _, w := range strings.Fields(q) {
		if len([]rune(w)) >= 3 {
			terms = append(terms, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
		}
	}
	if len(terms) == 0 {
		return []Memory{}, nil
	}
	rows, err := db.Query(`
SELECT m.id, m.folder, m.content, m.created_at, m.updated_at
FROM memories_fts JOIN memories m ON m.id = memories_fts.rowid
WHERE memories_fts MATCH ? AND m.user_id = ?
ORDER BY rank LIMIT 20`, strings.Join(terms, " OR "), userID)
	if err != nil {
		return nil, err
	}
	return scanMemories(rows)
}

func updateMemory(db *sql.DB, id int64, userID, content string) (bool, error) {
	res, err := db.Exec(`UPDATE memories SET content = ?, updated_at = ? WHERE id = ? AND user_id = ?`, content, time.Now().UnixMilli(), id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func deleteMemory(db *sql.DB, id int64, userID string) (bool, error) {
	res, err := db.Exec(`DELETE FROM memories WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type WebSearch struct {
	Query             string `json:"query"`
	At                int64  `json:"at"`
	ConversationID    string `json:"conversation_id"`
	ConversationTitle string `json:"conversation_title"`
}

// listWebSearches reads the user's `@web` history back from the saved
// result messages, newest first. A deleted chat takes its searches with it.
func listWebSearches(db *sql.DB, userID string, limit int) ([]WebSearch, error) {
	rows, err := db.Query(
		`SELECT m.content, m.created_at, c.id, c.title FROM messages m
		 JOIN conversations c ON c.id = m.conversation_id
		 WHERE c.user_id = ? AND m.role = 'system' AND m.content LIKE ?
		 ORDER BY m.created_at DESC LIMIT ?`, userID, webResultsPrefix+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []WebSearch{}
	for rows.Next() {
		var content string
		var ws WebSearch
		if err := rows.Scan(&content, &ws.At, &ws.ConversationID, &ws.ConversationTitle); err != nil {
			return nil, err
		}
		query, _, ok := parseSearchResults(content)
		if !ok {
			continue
		}
		ws.Query = query
		out = append(out, ws)
	}
	return out, rows.Err()
}

func countWebSearchesSince(db *sql.DB, userID string, since int64) (int, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM messages m JOIN conversations c ON c.id = m.conversation_id
		 WHERE c.user_id = ? AND m.role = 'system' AND m.content LIKE ? AND m.created_at >= ?`,
		userID, webResultsPrefix+"%", since,
	).Scan(&n)
	return n, err
}
