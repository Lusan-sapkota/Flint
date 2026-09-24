package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestDecodeUploadsRejectsBeforeWriting(t *testing.T) {
	ok := AttachmentUpload{Data: "data:text/plain;base64," + base64.StdEncoding.EncodeToString([]byte("hi")), Filename: "a.txt"}
	big := AttachmentUpload{Data: base64.StdEncoding.EncodeToString(make([]byte, maxAttachmentBytes+1)), Filename: "big.bin"}

	if got, err := decodeUploads([]AttachmentUpload{ok, ok}); err != nil || len(got) != 2 || string(got[0].data) != "hi" {
		t.Fatalf("valid uploads: got %v, %v", got, err)
	}
	if _, err := decodeUploads([]AttachmentUpload{ok, big}); err == nil || !strings.Contains(err.Error(), "big.bin") {
		t.Errorf("oversized upload should be rejected by name, got %v", err)
	}
	if _, err := decodeUploads([]AttachmentUpload{ok, {Data: "!!!", Filename: "bad"}}); err == nil {
		t.Error("invalid base64 should be rejected")
	}
	if _, err := decodeUploads(make([]AttachmentUpload, maxAttachmentsPerTurn+1)); err == nil {
		t.Error("more than the per-turn cap should be rejected, not silently truncated")
	}
}
