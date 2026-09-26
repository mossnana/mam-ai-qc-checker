package contracts

// All messages use a CloudEvents-compatible envelope. The payload remains
// deliberately small; object bytes must never travel through NATS.
type Envelope[T any] struct {
	SpecVersion string `json:"specversion"`
	ID          string `json:"id"`
	Source      string `json:"source"`
	Type        string `json:"type"`
	Subject     string `json:"subject"`
	Time        string `json:"time"`
	TenantID    string `json:"tenantid"`
	Data        T      `json:"data"`
}

type RuleManifest struct {
	RuleType           string         `json:"ruleType"`
	Version            string         `json:"version"`
	DisplayName        string         `json:"displayName"`
	Engine             string         `json:"engine"`
	Subject            string         `json:"subject"`
	SupportedMIMETypes []string       `json:"supportedMimeTypes"`
	DefaultSeverity    string         `json:"defaultSeverity"`
	TimeoutSec         int            `json:"timeoutSec"`
	ParamSchema        map[string]any `json:"paramSchema"`
}

type SpecimenRef struct {
	ID        string `json:"id"`
	Revision  int    `json:"revision"`
	ObjectKey string `json:"objectKey"`
	MimeType  string `json:"mimeType"`
	SizeBytes int64  `json:"sizeBytes"`
	WidthPx   int    `json:"widthPx,omitempty"`
	HeightPx  int    `json:"heightPx,omitempty"`
	DPI       int    `json:"dpi,omitempty"`
	Format    string `json:"format,omitempty"`
}

type RuleSnapshot struct {
	ID       string         `json:"id"`
	Name     string         `json:"name,omitempty"`
	RuleType string         `json:"ruleType"`
	Enabled  bool           `json:"enabled"`
	Severity string         `json:"severity"`
	Blocking bool           `json:"blocking"`
	Params   map[string]any `json:"params"`
}

type CheckTaskRequest struct {
	JobID    string            `json:"jobId"`
	TaskID   string            `json:"taskId"`
	TenantID string            `json:"tenantId"`
	Specimen SpecimenRef       `json:"specimen"`
	Context  InspectionContext `json:"context"`
	Rule     RuleSnapshot      `json:"rule"`
	Attempt  int               `json:"attempt"`
}

// InspectionContext carries optional creative inputs without coupling the
// orchestrator to any particular visual checker implementation.
type InspectionContext struct {
	Brief              string `json:"brief,omitempty"`
	ReferenceObjectKey string `json:"referenceObjectKey,omitempty"`
}

type Finding struct {
	ID         string  `json:"id"`
	DefectType string  `json:"defectType"`
	Severity   string  `json:"severity"`
	Confidence float64 `json:"confidence"`
	Message    string  `json:"message"`
}

type Attribution struct {
	CheckerName    string `json:"checkerName"`
	CheckerVersion string `json:"checkerVersion"`
	ModelRevision  string `json:"modelRevision"`
	DurationMs     int64  `json:"durationMs"`
}

type CheckTaskCompleted struct {
	JobID       string      `json:"jobId"`
	TaskID      string      `json:"taskId"`
	RuleType    string      `json:"ruleType"`
	State       string      `json:"state"`
	Findings    []Finding   `json:"findings"`
	Attribution Attribution `json:"attribution"`
}
