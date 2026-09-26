package main

import (
	"archive/zip"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mam-ai-qc-checker/backend/internal/platform"
)

const (
	settingsAddress       = ":8084"
	maxAuthArchiveBytes   = 25 << 20
	maxAuthExpandedBytes  = 64 << 20
	maxAuthArchiveEntries = 1000
)

// runtimeConfig contains credentials only inside this service. It must never
// be serialized into a response or logged.
type runtimeConfig struct {
	Agent     string `json:"agent"`
	Model     string `json:"model,omitempty"`
	APIKey    string `json:"apiKey,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

type settingsStore struct {
	mu       sync.RWMutex
	config   runtimeConfig
	root     string
	configFn string
}

func newSettingsStore(root string) (*settingsStore, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create AI settings directory: %w", err)
	}
	store := &settingsStore{
		root:     root,
		configFn: filepath.Join(root, "settings.json"),
		config: runtimeConfig{
			Agent: configuredAgent(),
			Model: os.Getenv("AI_QC_MODEL"),
		},
	}
	data, err := os.ReadFile(store.configFn)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read AI settings: %w", err)
	}
	if err := json.Unmarshal(data, &store.config); err != nil {
		return nil, fmt.Errorf("parse AI settings: %w", err)
	}
	if err := validateAgent(store.config.Agent); err != nil {
		return nil, fmt.Errorf("invalid persisted AI settings: %w", err)
	}
	return store, nil
}

func (s *settingsStore) current() runtimeConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	config := s.config
	if config.APIKey == "" {
		switch config.Agent {
		case "codex-api":
			config.APIKey = os.Getenv("OPENAI_API_KEY")
		case "claude-code-api":
			config.APIKey = os.Getenv("ANTHROPIC_API_KEY")
		}
	}
	return config
}

func (s *settingsStore) cliAuthDir(agent string) string {
	if agent != "codex-cli" && agent != "claude-code-cli" {
		return ""
	}
	directory := filepath.Join(s.root, "cli-auth", agent)
	info, err := os.Stat(directory)
	if err == nil && info.IsDir() {
		return directory
	}
	return ""
}

func (s *settingsStore) update(agent, model, apiKey string) error {
	if err := validateAgent(agent); err != nil {
		return err
	}
	if len(model) > 120 {
		return errors.New("model name must be 120 characters or fewer")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(apiKey)
	if key == "" && s.config.Agent == agent {
		key = s.config.APIKey
	}
	if (agent == "codex-api" || agent == "claude-code-api") && key == "" {
		return errors.New("an API key is required when using an API agent")
	}
	if agent == "codex-cli" || agent == "claude-code-cli" {
		key = ""
	}
	s.config = runtimeConfig{Agent: agent, Model: strings.TrimSpace(model), APIKey: key, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	return s.persistLocked()
}

func (s *settingsStore) persistLocked() error {
	data, err := json.Marshal(s.config)
	if err != nil {
		return err
	}
	temporary := s.configFn + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.configFn)
}

func validateAgent(agent string) error {
	switch agent {
	case "codex-api", "codex-cli", "claude-code-api", "claude-code-cli":
		return nil
	default:
		return fmt.Errorf("unsupported agent %q", agent)
	}
}

type publicSettings struct {
	Agent        string `json:"agent"`
	Model        string `json:"model,omitempty"`
	APIKeyStored bool   `json:"apiKeyStored"`
	CLIAuthReady bool   `json:"cliAuthReady"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
}

func (s *settingsStore) public() publicSettings {
	config := s.current()
	return publicSettings{
		Agent:        config.Agent,
		Model:        config.Model,
		APIKeyStored: config.APIKey != "",
		CLIAuthReady: s.cliAuthDir(config.Agent) != "" || defaultCLIAuthAvailable(config.Agent),
		UpdatedAt:    config.UpdatedAt,
	}
}

func defaultCLIAuthAvailable(agent string) bool {
	var directory string
	if agent == "codex-cli" {
		directory = filepath.Join(os.Getenv("HOME"), ".codex")
	} else if agent == "claude-code-cli" {
		directory = os.Getenv("CLAUDE_CONFIG_DIR")
		if directory == "" {
			directory = filepath.Join(os.Getenv("HOME"), ".claude")
		}
	}
	info, err := os.Stat(directory)
	return err == nil && info.IsDir()
}

// authArchiveReader avoids accepting paths or symlinks from user-supplied ZIPs.
// The archive is extracted under a service-owned root and swapped atomically.
func (s *settingsStore) extractAuthArchive(agent string, reader *zip.Reader) error {
	if len(reader.File) == 0 || len(reader.File) > maxAuthArchiveEntries {
		return errors.New("authentication ZIP must contain between 1 and 1,000 files")
	}
	paths := make([]string, 0, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		name, err := safeZipPath(file.Name)
		if err != nil {
			return err
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if !file.FileInfo().Mode().IsRegular() {
			return fmt.Errorf("authentication ZIP entry %q is not a regular file", file.Name)
		}
		expanded += file.UncompressedSize64
		if expanded > maxAuthExpandedBytes {
			return errors.New("authentication ZIP expands beyond 64 MB")
		}
		paths = append(paths, name)
	}
	if len(paths) == 0 {
		return errors.New("authentication ZIP has no files")
	}
	prefix := sharedArchivePrefix(paths)
	temporary, err := os.MkdirTemp(filepath.Join(s.root, "cli-auth"), ".upload-")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(filepath.Join(s.root, "cli-auth"), 0o700); err != nil {
				return err
			}
			temporary, err = os.MkdirTemp(filepath.Join(s.root, "cli-auth"), ".upload-")
		}
		if err != nil {
			return err
		}
	}
	defer os.RemoveAll(temporary)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, _ := safeZipPath(file.Name)
		name = trimArchivePrefix(name, prefix, agent)
		if name == "" {
			continue
		}
		destination := filepath.Join(temporary, filepath.FromSlash(name))
		if !strings.HasPrefix(destination, temporary+string(os.PathSeparator)) {
			return errors.New("authentication ZIP contains an invalid destination")
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
			return err
		}
		input, err := file.Open()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
		if err == nil {
			_, err = io.Copy(output, input)
			closeErr := output.Close()
			if err == nil {
				err = closeErr
			}
		}
		_ = input.Close()
		if err != nil {
			return err
		}
	}
	destination := filepath.Join(s.root, "cli-auth", agent)
	backup := destination + ".previous"
	_ = os.RemoveAll(backup)
	if err := os.Rename(destination, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporary, destination); err != nil {
		_ = os.Rename(backup, destination)
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func safeZipPath(name string) (string, error) {
	clean := path.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "\\") {
		return "", fmt.Errorf("invalid authentication ZIP entry %q", name)
	}
	return clean, nil
}

func sharedArchivePrefix(paths []string) string {
	first := strings.Split(paths[0], "/")[0]
	for _, name := range paths {
		parts := strings.Split(name, "/")
		if len(parts) < 2 || parts[0] != first {
			return ""
		}
	}
	return first
}

func trimArchivePrefix(name, prefix, agent string) string {
	if prefix != "" {
		name = strings.TrimPrefix(name, prefix+"/")
	}
	expected := ".codex"
	if agent == "claude-code-cli" {
		expected = ".claude"
	}
	if strings.HasPrefix(name, expected+"/") {
		name = strings.TrimPrefix(name, expected+"/")
	}
	return name
}

func startSettingsServer(log *slog.Logger, settings *settingsStore) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/ai-settings", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeSettings(w, r, log) {
			return
		}
		switch r.Method {
		case http.MethodGet:
			platform.JSON(w, http.StatusOK, settings.public())
		case http.MethodPut:
			var input struct{ Agent, Model, APIKey string }
			if err := platform.DecodeJSON(r, &input); err != nil {
				platform.Error(log, w, r, 400, "INVALID_SETTINGS", "Invalid settings payload.", err)
				return
			}
			if err := settings.update(strings.ToLower(input.Agent), input.Model, input.APIKey); err != nil {
				platform.Error(log, w, r, 400, "INVALID_SETTINGS", err.Error(), nil)
				return
			}
			platform.JSON(w, http.StatusOK, settings.public())
		default:
			w.Header().Set("Allow", "GET, PUT, OPTIONS")
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/api/v1/ai-settings/cli-auth", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeSettings(w, r, log) {
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST, OPTIONS")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthArchiveBytes)
		if err := r.ParseMultipartForm(maxAuthArchiveBytes); err != nil {
			platform.Error(log, w, r, 400, "INVALID_ARCHIVE", "Upload a ZIP smaller than 25 MB.", err)
			return
		}
		agent := strings.ToLower(r.FormValue("agent"))
		file, _, err := r.FormFile("archive")
		if err != nil {
			platform.Error(log, w, r, 400, "INVALID_ARCHIVE", "Choose a CLI authentication ZIP.", err)
			return
		}
		defer file.Close()
		archive, err := io.ReadAll(file)
		if err != nil {
			platform.Error(log, w, r, 400, "INVALID_ARCHIVE", "Could not read the authentication ZIP.", err)
			return
		}
		reader, err := zip.NewReader(bytesReaderAt(archive), int64(len(archive)))
		if err != nil {
			platform.Error(log, w, r, 400, "INVALID_ARCHIVE", "The selected file is not a valid ZIP.", err)
			return
		}
		if err := settings.extractAuthArchive(agent, reader); err != nil {
			platform.Error(log, w, r, 400, "INVALID_ARCHIVE", "The authentication ZIP could not be accepted.", err)
			return
		}
		platform.JSON(w, http.StatusOK, settings.public())
	})
	go func() {
		server := &http.Server{Addr: settingsAddress, Handler: platform.HTTP(log, settingsCORS(mux)), ReadHeaderTimeout: 5 * time.Second}
		log.Info("AI settings API listening", "port", strings.TrimPrefix(settingsAddress, ":"))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("AI settings API stopped", "error", err)
		}
	}()
}

// bytesReaderAt implements io.ReaderAt without exposing the temporary upload.
type bytesReaderAt []byte

func (b bytesReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(b)) {
		return 0, io.EOF
	}
	n := copy(p, b[offset:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func authorizeSettings(w http.ResponseWriter, r *http.Request, log *slog.Logger) bool {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	expected := os.Getenv("SETTINGS_ADMIN_TOKEN")
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(r.Header.Get("X-Settings-Token"))) == 1 {
		return true
	}
	platform.Error(log, w, r, http.StatusUnauthorized, "SETTINGS_UNAUTHORIZED", "A valid settings access token is required.", nil)
	return false
}

func settingsCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := os.Getenv("SETTINGS_ALLOWED_ORIGIN")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Settings-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, POST, OPTIONS")
		next.ServeHTTP(w, r)
	})
}
