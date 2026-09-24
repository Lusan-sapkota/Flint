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
	BraveAPIKey     *string  `json:"brave_api_key,omitempty"`
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
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
}

type Message struct {
	ID          int64        `json:"id"`
	Role        string       `json:"role"`
	Content     string       `json:"content"`
	ToolCalls   *string      `json:"tool_calls,omitempty"`
	ToolCallID  *string      `json:"tool_call_id,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
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
	if _, err := db.Exec(`ALTER TABLE attachments ADD COLUMN filename TEXT NOT NULL DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

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
		`SELECT id, full_name, email, password_hash, ollama_base_url, preferred_models, brave_api_key, created_at, updated_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.CreatedAt, &u.UpdatedAt)
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
		`SELECT id, full_name, email, password_hash, ollama_base_url, preferred_models, brave_api_key, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.CreatedAt, &u.UpdatedAt)
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
		`SELECT u.id, u.full_name, u.email, u.password_hash, u.ollama_base_url, u.preferred_models, u.brave_api_key, u.created_at, u.updated_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?`, sessionID,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &preferredModelsRaw, &u.BraveAPIKey, &u.CreatedAt, &u.UpdatedAt, &expiresAt)
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
	err := db.QueryRow(
		`SELECT id, user_id, title, model, attached_folder, created_at, updated_at FROM conversations WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.AttachedFolder, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT id, role, content, tool_calls, tool_call_id, created_at FROM messages WHERE conversation_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID, &m.CreatedAt); err != nil {
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

	return &ConversationWithMessages{Conversation: c, Messages: messages}, nil
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

func createAttachment(db *sql.DB, id string, messageID int64, mimeType, filename, filePath string) error {
	_, err := db.Exec(
		`INSERT INTO attachments (id, message_id, mime_type, filename, file_path, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, messageID, mimeType, filename, filePath, time.Now().UnixMilli(),
	)
	return err
}

func getAttachmentPathsForConversation(db *sql.DB, conversationID string) ([]string, error) {
	rows, err := db.Query(
		`SELECT a.file_path FROM attachments a JOIN messages m ON m.id = a.message_id WHERE m.conversation_id = ?`, conversationID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	paths := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
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

func insertToolCallMessage(db *sql.DB, conversationID, content, toolCallsJSON string) error {
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, tool_calls, created_at) VALUES (?, 'assistant', ?, ?, ?)`,
		conversationID, content, toolCallsJSON, time.Now().UnixMilli(),
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
