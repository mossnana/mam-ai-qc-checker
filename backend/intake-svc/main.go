package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mam-ai-qc-checker/backend/internal/contracts"
	"github.com/mam-ai-qc-checker/backend/internal/platform"
)

const maxUploadSize = 100 << 20

type presignRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"contentType"`
	SizeBytes   int64  `json:"sizeBytes"`
}

func main() {
	log := platform.Logger("intake-svc")
	root := valueOr("OBJECT_ROOT", "/objects")
	publicBase := strings.TrimSuffix(valueOr("UPLOAD_PUBLIC_BASE_URL", "http://localhost:8081"), "/")
	if err := os.MkdirAll(root, 0o750); err != nil {
		log.Error("cannot create object root", "error", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { platform.JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		platform.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /api/v1/uploads:presign", func(w http.ResponseWriter, r *http.Request) {
		var input presignRequest
		if err := platform.DecodeJSON(r, &input); err != nil {
			platform.Error(log, w, r, http.StatusBadRequest, "INVALID_UPLOAD_REQUEST", "A valid upload request is required.", err)
			return
		}
		if input.Filename == "" {
			platform.Error(log, w, r, http.StatusBadRequest, "MISSING_FILENAME", "A file name is required.", nil)
			return
		}
		if input.SizeBytes > maxUploadSize {
			platform.Error(log, w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "File exceeds the 100 MB development limit.", nil)
			return
		}
		id := platform.NewID()
		platform.JSON(w, http.StatusCreated, map[string]any{"uploadUrl": fmt.Sprintf("%s/api/v1/uploads/%s", publicBase, id), "objectKey": "local/" + id, "expiresIn": 900})
	})
	mux.HandleFunc("PUT /api/v1/uploads/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" || strings.Contains(id, "/") {
			platform.Error(log, w, r, http.StatusBadRequest, "INVALID_UPLOAD_ID", "The upload ID is invalid.", nil)
			return
		}
		if r.ContentLength > maxUploadSize {
			platform.Error(log, w, r, http.StatusRequestEntityTooLarge, "UPLOAD_TOO_LARGE", "File exceeds the 100 MB development limit.", nil)
			return
		}
		path := filepath.Join(root, id)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o640)
		if err != nil {
			platform.Error(log, w, r, http.StatusInternalServerError, "OBJECT_CREATE_FAILED", "Unable to prepare storage for this file.", err)
			return
		}
		hash := sha256.New()
		// Persist the stream and calculate its checksum in one pass. Writing only
		// to the hash leaves an empty object, which image decoding cannot inspect.
		written, copyErr := io.Copy(io.MultiWriter(file, hash), http.MaxBytesReader(w, r.Body, maxUploadSize))
		closeErr := file.Close()
		if copyErr != nil || closeErr != nil {
			_ = os.Remove(path)
			if copyErr != nil {
				platform.Error(log, w, r, http.StatusBadRequest, "OBJECT_WRITE_FAILED", "Unable to store the uploaded file.", copyErr)
			} else {
				platform.Error(log, w, r, http.StatusInternalServerError, "OBJECT_CLOSE_FAILED", "Unable to finalize the uploaded file.", closeErr)
			}
			return
		}
		stored, err := os.Open(path)
		if err != nil {
			platform.Error(log, w, r, http.StatusInternalServerError, "OBJECT_OPEN_FAILED", "Unable to inspect the uploaded file.", err)
			return
		}
		defer stored.Close()
		config, format, err := image.DecodeConfig(stored)
		if err != nil {
			_ = os.Remove(path)
			platform.Error(log, w, r, http.StatusUnsupportedMediaType, "UNSUPPORTED_IMAGE", "Only PNG, JPEG, and GIF images are supported in this MVP.", err)
			return
		}
		mimeType := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif"}[format]
		platform.JSON(w, http.StatusCreated, contracts.SpecimenRef{ID: id, Revision: 1, ObjectKey: "local/" + id, MimeType: mimeType, SizeBytes: written, WidthPx: config.Width, HeightPx: config.Height, Format: format})
		log.Info("specimen stored", "object_key", "local/"+id, "bytes", written, "checksum", hex.EncodeToString(hash.Sum(nil)))
	})
	srv := &http.Server{Addr: ":8081", Handler: platform.HTTP(log, cors(mux)), ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "port", 8081)
	log.Error("server stopped", "error", srv.ListenAndServe())
}

func valueOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
