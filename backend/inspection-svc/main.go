package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/mam-ai-qc-checker/backend/internal/contracts"
	"github.com/mam-ai-qc-checker/backend/internal/platform"
	"github.com/nats-io/nats.go"
)

type task struct {
	ID          string                 `json:"id"`
	RuleID      string                 `json:"ruleId"`
	RuleName    string                 `json:"ruleName,omitempty"`
	RuleType    string                 `json:"ruleType"`
	State       string                 `json:"state"`
	Findings    []contracts.Finding    `json:"findings"`
	Attribution contracts.Attribution  `json:"attribution"`
	Rule        contracts.RuleSnapshot `json:"-"`
}
type job struct {
	ID         string                      `json:"id"`
	TenantID   string                      `json:"tenantId"`
	ProjectID  string                      `json:"projectId"`
	Status     string                      `json:"status"`
	Verdict    string                      `json:"verdict,omitempty"`
	Specimen   contracts.SpecimenRef       `json:"specimen"`
	Context    contracts.InspectionContext `json:"context"`
	Rules      []contracts.RuleSnapshot    `json:"rules"`
	Tasks      map[string]*task            `json:"tasks"`
	CreatedAt  time.Time                   `json:"createdAt"`
	FinishedAt *time.Time                  `json:"finishedAt,omitempty"`
}
type store struct {
	sync.RWMutex
	jobs map[string]*job
}

func main() {
	log := platform.Logger("inspection-svc")
	nc, err := platform.Connect("inspection-svc")
	if err != nil {
		log.Error("nats connection failed", "error", err)
		return
	}
	defer nc.Close()
	s := &store{jobs: map[string]*job{}}
	_, err = nc.Subscribe("qc.inspection.task.completed.v1", func(msg *nats.Msg) {
		var result contracts.CheckTaskCompleted
		if err := json.Unmarshal(msg.Data, &result); err != nil {
			log.Error("invalid check result", "subject", msg.Subject, "error", err)
			return
		}
		s.complete(result)
		log.Info("task completed", "job_id", result.JobID, "task_id", result.TaskID)
	})
	if err != nil {
		log.Error("result subscription failed", "error", err)
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { platform.JSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		platform.JSON(w, 200, map[string]string{"status": "ready"})
	})
	mux.HandleFunc("POST /api/v1/inspections", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			TenantID  string                      `json:"tenantId"`
			ProjectID string                      `json:"projectId"`
			Specimen  contracts.SpecimenRef       `json:"specimen"`
			Context   contracts.InspectionContext `json:"context"`
			Rules     []contracts.RuleSnapshot    `json:"rules"`
		}
		if err := platform.DecodeJSON(r, &input); err != nil {
			platform.Error(log, w, r, http.StatusBadRequest, "INVALID_INSPECTION_REQUEST", "A valid inspection request is required.", err)
			return
		}
		if input.ProjectID == "" || input.Specimen.ID == "" {
			platform.Error(log, w, r, http.StatusBadRequest, "MISSING_INSPECTION_FIELDS", "projectId and specimen.id are required.", nil)
			return
		}
		j := &job{ID: platform.NewID(), TenantID: input.TenantID, ProjectID: input.ProjectID, Status: "ANALYZING", Specimen: input.Specimen, Context: input.Context, Rules: input.Rules, Tasks: map[string]*task{}, CreatedAt: time.Now().UTC()}
		for _, rule := range input.Rules {
			if !rule.Enabled {
				continue
			}
			id := platform.NewID()
			j.Tasks[id] = &task{ID: id, RuleID: rule.ID, RuleName: rule.Name, RuleType: rule.RuleType, State: "PENDING", Rule: rule}
		}
		s.Lock()
		s.jobs[j.ID] = j
		for taskID, currentTask := range j.Tasks {
			request := contracts.CheckTaskRequest{JobID: j.ID, TaskID: taskID, TenantID: j.TenantID, Specimen: j.Specimen, Context: j.Context, Rule: currentTask.Rule, Attempt: 1}
			payload, err := json.Marshal(request)
			if err != nil {
				currentTask.State = "FAILED"
				log.Error("cannot serialize check task", "job_id", j.ID, "task_id", taskID, "error", err)
				continue
			}
			subject := subjectFor(currentTask.RuleType)
			if subject == "" {
				currentTask.State = "SKIPPED"
				log.Warn("rule type has no registered dispatch subject", "job_id", j.ID, "task_id", taskID, "rule_type", currentTask.RuleType)
				continue
			}
			if err := nc.Publish(subject, payload); err != nil {
				currentTask.State = "FAILED"
				log.Error("cannot publish check task", "job_id", j.ID, "task_id", taskID, "subject", subject, "error", err)
			}
		}
		if len(j.Tasks) == 0 {
			now := time.Now().UTC()
			j.Status = "EVALUATED"
			j.Verdict = "PASSED"
			j.FinishedAt = &now
		}
		s.Unlock()
		log.Info("inspection job created", "job_id", j.ID, "project_id", j.ProjectID, "task_count", len(j.Tasks))
		platform.JSON(w, http.StatusAccepted, map[string]string{"jobId": j.ID, "status": j.Status})
	})
	mux.HandleFunc("GET /api/v1/inspections/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.RLock()
		j, ok := s.jobs[r.PathValue("id")]
		s.RUnlock()
		if !ok {
			platform.Error(log, w, r, http.StatusNotFound, "INSPECTION_NOT_FOUND", "The inspection job was not found.", nil)
			return
		}
		platform.JSON(w, 200, j)
	})
	srv := &http.Server{Addr: ":8083", Handler: platform.HTTP(log, cors(mux)), ReadHeaderTimeout: 5 * time.Second}
	log.Info("listening", "port", 8083)
	log.Error("server stopped", "error", srv.ListenAndServe())
}

func (s *store) complete(result contracts.CheckTaskCompleted) {
	s.Lock()
	defer s.Unlock()
	j := s.jobs[result.JobID]
	if j == nil {
		return
	}
	t := j.Tasks[result.TaskID]
	if t == nil || t.State == "DONE" {
		return
	}
	t.State = result.State
	t.Findings = result.Findings
	t.Attribution = result.Attribution
	for _, other := range j.Tasks {
		if other.State == "PENDING" || other.State == "RUNNING" {
			return
		}
	}
	now := time.Now().UTC()
	j.Status = "EVALUATED"
	j.FinishedAt = &now
	j.Verdict = verdict(j)
}
func verdict(j *job) string {
	for _, t := range j.Tasks {
		if t.State == "FAILED" {
			return "NEEDS_REVIEW"
		}
		for _, f := range t.Findings {
			if f.Severity == "CRITICAL" {
				return "FAILED"
			}
		}
	}
	for _, t := range j.Tasks {
		for _, f := range t.Findings {
			if f.Severity == "MAJOR" {
				return "FAILED"
			}
		}
	}
	return "PASSED"
}
func subjectFor(ruleType string) string {
	switch ruleType {
	case "IMAGE_DIMENSION":
		return "qc.check.dimension.v1"
	case "FILE_SIZE_LIMIT":
		return "qc.check.filesize.v1"
	case "COLOR_PALETTE_COMPLIANCE":
		return "qc.check.color.v1"
	case "VISUAL_AI_QC":
		return "qc.check.visual-ai.v1"
	default:
		return ""
	}
}
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Max-Age", "600")
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
