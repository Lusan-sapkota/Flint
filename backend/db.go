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
	ID        string `json:"id"`
	UserID    string `json:"-"`
	Title     string `json:"title"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at,omitempty"`
}

type ConversationWithMessages struct {
	Conversation
	Messages []Message `json:"messages"`
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
	id         TEXT PRIMARY KEY,
	user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	title      TEXT NOT NULL,
	model      TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	conversation_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
	role            TEXT NOT NULL,
	content         TEXT NOT NULL,
	created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at);
CREATE INDEX IF NOT EXISTS idx_conversations_user_id ON conversations(user_id);
CREATE INDEX IF NOT EXISTS idx_conversations_updated_at ON conversations(updated_at DESC);
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
		`SELECT id, user_id, title, model, created_at, updated_at FROM conversations WHERE user_id = ? ORDER BY updated_at DESC`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func getConversation(db *sql.DB, id, userID string) (*ConversationWithMessages, error) {
	var c Conversation
	err := db.QueryRow(
		`SELECT id, user_id, title, model, created_at, updated_at FROM conversations WHERE id = ? AND user_id = ?`, id, userID,
	).Scan(&c.ID, &c.UserID, &c.Title, &c.Model, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := db.Query(
		`SELECT role, content, created_at FROM messages WHERE conversation_id = ? ORDER BY id ASC`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	messages := []Message{}
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &ConversationWithMessages{Conversation: c, Messages: messages}, nil
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
