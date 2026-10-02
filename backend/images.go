package main

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	maxAttachmentBytes    = 8 * 1024 * 1024
	maxAttachmentsPerTurn = 4
	maxMessageBody        = maxAttachmentsPerTurn*maxAttachmentBytes*4/3 + 8*1024*1024
)

// Each format verified to decode in Ollama's vision path; keep SUPPORTED_IMAGE_TYPES in chat.js in sync.
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

// Filename is for display only, never trusted: the stored blob name is always a fresh UUID.
type AttachmentUpload struct {
	Data     string `json:"data"`
	Filename string `json:"filename"`
}

// Any file type is stored; only images reach the model, others become a text note (toOllamaMessage).
type decodedUpload struct {
	data     []byte
	filename string
}

// Validates all uploads before writing any, so one bad file never leaves a partial save.
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

// Measured on qwen3.5-4b as width*height/1024; errs high with a floor, flat guess for webp/bmp.
func imageTokens(b64 string) int {
	cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(b64)))
	if err != nil {
		return 1024
	}
	return int(math.Max(256, float64(cfg.Width*cfg.Height)/1024))
}
