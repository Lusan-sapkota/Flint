package main

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type User struct {
	ID            string  `json:"id"`
	FullName      string  `json:"full_name"`
	Email         string  `json:"email"`
	PasswordHash  string  `json:"-"`
	OllamaBaseURL *string `json:"ollama_base_url,omitempty"`
	CreatedAt     int64   `json:"created_at"`
	UpdatedAt     int64   `json:"updated_at"`
}

type Session struct {
	ID        string
	UserID    string
	ExpiresAt int64
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
	Role       string  `json:"role"`
	Content    string  `json:"content"`
	ToolCalls  *string `json:"tool_calls,omitempty"`
	ToolCallID *string `json:"tool_call_id,omitempty"`
	CreatedAt  int64   `json:"created_at,omitempty"`
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
	id              TEXT PRIMARY KEY,
	full_name       TEXT NOT NULL,
	email           TEXT NOT NULL UNIQUE,
	password_hash   TEXT NOT NULL,
	ollama_base_url TEXT,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);

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

CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON conversations(user_id);
CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_commands_conversation_id ON commands(conversation_id);
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
	return db, nil
}

func createUser(db *sql.DB, id, fullName, email, passwordHash string) (User, error) {
	now := time.Now().UnixMilli()
	u := User{ID: id, FullName: fullName, Email: email, PasswordHash: passwordHash, CreatedAt: now, UpdatedAt: now}
	_, err := db.Exec(
		`INSERT INTO users (id, full_name, email, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		u.ID, u.FullName, u.Email, u.PasswordHash, u.CreatedAt, u.UpdatedAt,
	)
	return u, err
}

func getUserByEmail(db *sql.DB, email string) (*User, error) {
	var u User
	err := db.QueryRow(
		`SELECT id, full_name, email, password_hash, ollama_base_url, created_at, updated_at FROM users WHERE email = ?`, email,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func getUserByID(db *sql.DB, id string) (*User, error) {
	var u User
	err := db.QueryRow(
		`SELECT id, full_name, email, password_hash, ollama_base_url, created_at, updated_at FROM users WHERE id = ?`, id,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func updateUserOllamaURL(db *sql.DB, userID string, baseURL *string) error {
	_, err := db.Exec(
		`UPDATE users SET ollama_base_url = ?, updated_at = ? WHERE id = ?`,
		baseURL, time.Now().UnixMilli(), userID,
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
	var expiresAt int64
	err := db.QueryRow(
		`SELECT u.id, u.full_name, u.email, u.password_hash, u.ollama_base_url, u.created_at, u.updated_at, s.expires_at
		 FROM sessions s JOIN users u ON u.id = s.user_id
		 WHERE s.id = ?`, sessionID,
	).Scan(&u.ID, &u.FullName, &u.Email, &u.PasswordHash, &u.OllamaBaseURL, &u.CreatedAt, &u.UpdatedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt < time.Now().UnixMilli() {
		return nil, nil
	}
	return &u, nil
}

func deleteSession(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
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
		`SELECT role, content, tool_calls, tool_call_id, created_at FROM messages WHERE conversation_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content, &m.ToolCalls, &m.ToolCallID, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
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

func insertMessage(db *sql.DB, conversationID, role, content string) error {
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
		conversationID, role, content, time.Now().UnixMilli(),
	)
	return err
}

func insertToolCallMessage(db *sql.DB, conversationID, toolCallsJSON string) error {
	_, err := db.Exec(
		`INSERT INTO messages (conversation_id, role, content, tool_calls, created_at) VALUES (?, 'assistant', '', ?, ?)`,
		conversationID, toolCallsJSON, time.Now().UnixMilli(),
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

func maybeSetTitle(db *sql.DB, id, firstMessage string) error {
	title := firstMessage
	if len(title) > 60 {
		title = title[:60]
	}
	_, err := db.Exec(`UPDATE conversations SET title = ? WHERE id = ? AND title = 'New chat'`, title, id)
	return err
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

func resolveCommand(db *sql.DB, id, status, output string, exitCode *int) error {
	_, err := db.Exec(
		`UPDATE commands SET status = ?, output = ?, exit_code = ?, decided_at = ? WHERE id = ?`,
		status, output, exitCode, time.Now().UnixMilli(), id,
	)
	return err
}
