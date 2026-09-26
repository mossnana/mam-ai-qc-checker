package main

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestExtractAuthArchiveFlattensProfileDirectory(t *testing.T) {
	store, err := newSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipBytes(t, map[string]string{"backup/.codex/auth.json": "credential"})
	reader, err := zip.NewReader(bytesReaderAt(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.extractAuthArchive("codex-cli", reader); err != nil {
		t.Fatal(err)
	}
	if got := store.cliAuthDir("codex-cli"); got == "" {
		t.Fatal("expected uploaded Codex profile directory")
	}
}

func TestExtractAuthArchiveRejectsTraversal(t *testing.T) {
	store, err := newSettingsStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := zipBytes(t, map[string]string{"../auth.json": "credential"})
	reader, err := zip.NewReader(bytesReaderAt(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.extractAuthArchive("codex-cli", reader); err == nil {
		t.Fatal("expected traversal ZIP to be rejected")
	}
}

func zipBytes(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
