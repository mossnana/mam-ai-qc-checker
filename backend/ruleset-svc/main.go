package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/mam-ai-qc-checker/backend/internal/contracts"
	"github.com/mam-ai-qc-checker/backend/internal/platform"
	"github.com/nats-io/nats.go"
)

type ruleSet struct {
	ID, ProjectID, Name, Status string
	Version                     int
	Rules                       []contracts.RuleSnapshot `json:"rules"`
}
type registry struct {
	sync.RWMutex
	manifests map[string]contracts.RuleManifest
	sets      map[string]ruleSet
}

func main() {
	log := platform.Logger("ruleset-svc")
	nc, err := platform.Connect("ruleset-svc")
	if err != nil {
		log.Error("nats connection failed", "error", err)
		return
	}
	defer nc.Close()
	r := &registry{manifests: map[string]contracts.RuleManifest{}, sets: map[string]ruleSet{}}
	_, err = nc.Subscribe("qc.registry.announce", func(m *nats.Msg) {
		var manifest contracts.RuleManifest
		if err := json.Unmarshal(m.Data, &manifest); err != nil {
			log.Error("invalid checker manifest", "subject", m.Subject, "error", err)
			return
		}
		if manifest.RuleType != "" {
			r.Lock()
			r.manifests[manifest.RuleType] = manifest
			r.Unlock()
			log.Info("checker manifest registered", "rule_type", manifest.RuleType, "version", manifest.Version)
		} else {
			log.Warn("checker manifest missing rule type", "subject", m.Subject)
		}
	})
	if err != nil {
		log.Error("registry subscription failed", "error", err)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		platform.JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		platform.JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("GET /api/v1/rule-types", func(w http.ResponseWriter, _ *http.Request) {
		r.RLock()
		values := make([]contracts.RuleManifest, 0, len(r.manifests))
		for _, v := range r.manifests {
			values = append(values, v)
		}
		r.RUnlock()
		sort.Slice(values, func(i, j int) bool { return values[i].DisplayName < values[j].DisplayName })
		platform.JSON(w, http.StatusOK, values)
	})
	mux.HandleFunc("POST /api/v1/projects/{projectID}/rule-sets", func(w http.ResponseWriter, req *http.Request) {
		var input struct {
			Name  string                   `json:"name"`
			Rules []contracts.RuleSnapshot `json:"rules"`
		}
		if err := platform.DecodeJSON(req, &input); err != nil {
			platform.Error(log, w, req, http.StatusBadRequest, "INVALID_RULESET_REQUEST", "A valid rule set request is required.", err)
			return
		}
		if input.Name == "" {
			platform.Error(log, w, req, http.StatusBadRequest, "MISSING_RULESET_NAME", "A rule set name is required.", nil)
			return
		}
		id := platform.NewID()
		set := ruleSet{ID: id, ProjectID: req.PathValue("projectID"), Name: input.Name, Status: "DRAFT", Version: 1, Rules: input.Rules}
		r.Lock()
		r.sets[id] = set
		r.Unlock()
		platform.JSON(w, http.StatusCreated, set)
	})
	mux.HandleFunc("GET /api/v1/rule-sets/{id}", func(w http.ResponseWriter, req *http.Request) {
		r.RLock()
		set, ok := r.sets[req.PathValue("id")]
		r.RUnlock()
		if !ok {
			platform.Error(log, w, req, http.StatusNotFound, "RULESET_NOT_FOUND", "The rule set was not found.", nil)
			return
		}
		platform.JSON(w, 200, set)
	})
	mux.HandleFunc("POST /api/v1/rule-sets/{id}/publish", func(w http.ResponseWriter, req *http.Request) {
		r.Lock()
		set, ok := r.sets[req.PathValue("id")]
		if ok {
			set.Status = "PUBLISHED"
			r.sets[set.ID] = set
		}
		r.Unlock()
		if !ok {
			platform.Error(log, w, req, http.StatusNotFound, "RULESET_NOT_FOUND", "The rule set was not found.", nil)
			return
		}
		platform.JSON(w, 200, set)
	})

	srv := &http.Server{Addr: ":8082", Handler: platform.HTTP(log, cors(mux)), ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "port", 8082)
	log.Error("server stopped", "error", srv.ListenAndServe())
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
