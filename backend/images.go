package main

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	maxAttachmentBytes    = 8 * 1024 * 1024
	maxAttachmentsPerTurn = 4
)

// Every format here was checked to decode in Ollama's vision path. Keep
// SUPPORTED_IMAGE_TYPES in chat.js in sync.
var imageMimeExtensions = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
	"image/bmp":  "bmp",
}

func isImageMime(mimeType string) bool {
	_, ok := imageMimeExtensions[mimeType]
	return ok
}

func stripDataURIPrefix(s string) string {
	if idx := strings.Index(s, ","); idx != -1 && strings.HasPrefix(s, "data:") {
		return s[idx+1:]
	}
	return s
}

// AttachmentUpload is one file from a message's attachments field: base64
// (optionally data-URI-prefixed) content plus the original filename the
// browser reported, kept only for display/download - never trusted for
// anything else (the stored blob name is always a fresh UUID).
type AttachmentUpload struct {
	Data     string `json:"data"`
	Filename string `json:"filename"`
}

// saveAttachments stores any file type, not just images - a message can
// attach code, documents, archives, etc. Only image types (the fixed set
// Ollama's vision API accepts) end up in the model's context, via
// toOllamaMessage in context.go; anything else is retained purely for the
// human to see and download, with a plain-text note so the model at least
// knows a file was attached (see toOllamaMessage).
type decodedUpload struct {
	data     []byte
	filename string
}

// decodeUploads validates every upload before anything is written, so one
// bad file rejects the whole message instead of leaving it saved with only
// some of its attachments.
func decodeUploads(uploads []AttachmentUpload) ([]decodedUpload, error) {
	if len(uploads) > maxAttachmentsPerTurn {
		return nil, fmt.Errorf("at most %d attachments per message", maxAttachmentsPerTurn)
	}
	out := make([]decodedUpload, 0, len(uploads))
	for _, u := range uploads {
		data, err := base64.StdEncoding.DecodeString(stripDataURIPrefix(u.Data))
		if err != nil {
			return nil, fmt.Errorf("%s: invalid attachment data", u.Filename)
		}
		if len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("%s is over the %d MB attachment limit", u.Filename, maxAttachmentBytes/(1024*1024))
		}
		out = append(out, decodedUpload{data: data, filename: u.Filename})
	}
	return out, nil
}

func saveAttachments(attachmentsDir string, db *sql.DB, messageID int64, uploads []decodedUpload) error {
	for _, u := range uploads {
		mimeType := http.DetectContentType(u.data)

		id := uuid.NewString()
		ext := filepath.Ext(filepath.Base(u.filename))
		filename := id + ext
		fullPath := filepath.Join(attachmentsDir, filename)
		if err := os.WriteFile(fullPath, u.data, 0o644); err != nil {
			return fmt.Errorf("saving attachment: %w", err)
		}

		displayName := strings.TrimSpace(u.filename)
		if displayName == "" {
			displayName = filename
		}
		if r := []rune(displayName); len(r) > 200 {
			displayName = string(r[:200])
		}

		if err := createAttachment(db, id, messageID, mimeType, displayName, filename); err != nil {
			os.Remove(fullPath)
			return err
		}
	}
	return nil
}

func loadAttachmentBase64(attachmentsDir string, a Attachment) (string, error) {
	data, err := os.ReadFile(filepath.Join(attachmentsDir, a.FilePath))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
