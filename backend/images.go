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
	maxImageBytes    = 8 * 1024 * 1024
	maxImagesPerTurn = 4
)

var imageMimeExtensions = map[string]string{
	"image/png":  "png",
	"image/jpeg": "jpg",
	"image/gif":  "gif",
	"image/webp": "webp",
}

func stripDataURIPrefix(s string) string {
	if idx := strings.Index(s, ","); idx != -1 && strings.HasPrefix(s, "data:") {
		return s[idx+1:]
	}
	return s
}

func saveImageAttachments(attachmentsDir string, db *sql.DB, messageID int64, images []string) error {
	if len(images) > maxImagesPerTurn {
		images = images[:maxImagesPerTurn]
	}

	for _, raw := range images {
		data, err := base64.StdEncoding.DecodeString(stripDataURIPrefix(raw))
		if err != nil {
			return fmt.Errorf("invalid base64 image data: %w", err)
		}
		if len(data) > maxImageBytes {
			return fmt.Errorf("image exceeds the %d byte limit", maxImageBytes)
		}

		mimeType := http.DetectContentType(data)
		ext, ok := imageMimeExtensions[mimeType]
		if !ok {
			return fmt.Errorf("unsupported image type: %s", mimeType)
		}

		id := uuid.NewString()
		filename := id + "." + ext
		fullPath := filepath.Join(attachmentsDir, filename)
		if err := os.WriteFile(fullPath, data, 0o644); err != nil {
			return fmt.Errorf("saving attachment: %w", err)
		}

		if err := createAttachment(db, id, messageID, mimeType, filename); err != nil {
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
