package checker

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mam-ai-qc-checker/backend/internal/contracts"
	"github.com/mam-ai-qc-checker/backend/internal/platform"
	"github.com/nats-io/nats.go"
)

type Executor func(contracts.CheckTaskRequest) []contracts.Finding

func Run(service, subject string, manifest contracts.RuleManifest, execute Executor) error {
	log := platform.Logger(service)
	nc, err := platform.Connect(service)
	if err != nil {
		log.Error("nats connection failed", "error", err)
		return err
	}
	defer nc.Close()
	announce := func() {
		payload, _ := json.Marshal(manifest)
		if err := nc.Publish("qc.registry.announce", payload); err != nil {
			log.Error("manifest announcement failed", "error", err)
		}
	}
	announce()
	// A short second announcement makes local Compose startup deterministic when
	// the registry and checker containers cross their startup boundary.
	go func() { time.Sleep(2 * time.Second); announce() }()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	go func() {
		for range ticker.C {
			announce()
		}
	}()
	_, err = nc.QueueSubscribe(subject, service, func(msg *nats.Msg) {
		started := time.Now()
		var request contracts.CheckTaskRequest
		if err := json.Unmarshal(msg.Data, &request); err != nil {
			log.Error("invalid check task", "error", err)
			return
		}
		findings := execute(request)
		if findings == nil {
			findings = []contracts.Finding{}
		}
		result := contracts.CheckTaskCompleted{JobID: request.JobID, TaskID: request.TaskID, RuleType: request.Rule.RuleType, State: "DONE", Findings: findings, Attribution: contracts.Attribution{CheckerName: service, CheckerVersion: manifest.Version, ModelRevision: "n/a", DurationMs: time.Since(started).Milliseconds()}}
		payload, err := json.Marshal(result)
		if err != nil {
			log.Error("cannot serialize check result", "job_id", request.JobID, "task_id", request.TaskID, "error", err)
			return
		}
		if err := nc.Publish("qc.inspection.task.completed.v1", payload); err != nil {
			log.Error("result publication failed", "task_id", request.TaskID, "error", err)
		}
	})
	if err != nil {
		log.Error("checker subscription failed", "subject", subject, "error", err)
		return err
	}
	if err := nc.Flush(); err != nil {
		log.Error("checker subscription flush failed", "subject", subject, "error", err)
		return err
	}
	log.Info("checker ready", "subject", subject)
	select {}
}

func Number(params map[string]any, name string) (float64, bool) {
	value, ok := params[name]
	if !ok {
		return 0, false
	}
	number, ok := value.(float64)
	return number, ok
}
func StringList(params map[string]any, name string) []string {
	raw, ok := params[name].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			values = append(values, text)
		}
	}
	return values
}
func DimensionManifest() contracts.RuleManifest {
	return contracts.RuleManifest{RuleType: "IMAGE_DIMENSION", Version: "0.1.0", DisplayName: "ตรวจขนาดภาพ", Engine: "go", Subject: "qc.check.dimension.v1", SupportedMIMETypes: []string{"image/png", "image/jpeg", "application/pdf"}, DefaultSeverity: "MAJOR", TimeoutSec: 5, ParamSchema: map[string]any{"type": "object", "properties": map[string]any{"minWidth": map[string]any{"type": "number", "title": "ความกว้างขั้นต่ำ (px)"}, "minHeight": map[string]any{"type": "number", "title": "ความสูงขั้นต่ำ (px)"}, "maxWidth": map[string]any{"type": "number", "title": "ความกว้างสูงสุด (px)"}, "maxHeight": map[string]any{"type": "number", "title": "ความสูงสูงสุด (px)"}, "exactWidth": map[string]any{"type": "number", "title": "ความกว้างที่ต้องการ (px)"}, "exactHeight": map[string]any{"type": "number", "title": "ความสูงที่ต้องการ (px)"}, "aspectRatio": map[string]any{"type": "number", "title": "อัตราส่วนกว้างต่อสูง"}, "minDPI": map[string]any{"type": "number", "title": "DPI ขั้นต่ำ"}}}}
}
func FileSizeManifest() contracts.RuleManifest {
	return contracts.RuleManifest{RuleType: "FILE_SIZE_LIMIT", Version: "0.1.0", DisplayName: "ตรวจขนาดและชนิดไฟล์", Engine: "go", Subject: "qc.check.filesize.v1", SupportedMIMETypes: []string{"image/png", "image/jpeg", "application/pdf"}, DefaultSeverity: "CRITICAL", TimeoutSec: 5, ParamSchema: map[string]any{"type": "object", "properties": map[string]any{"maxSizeMB": map[string]any{"type": "number", "title": "ขนาดสูงสุด (MB)"}, "minSizeKB": map[string]any{"type": "number", "title": "ขนาดต่ำสุด (KB)"}, "allowedFormats": map[string]any{"type": "array", "title": "ชนิดไฟล์ที่อนุญาต", "items": map[string]any{"type": "string"}}}}}
}
func ColorManifest() contracts.RuleManifest {
	return contracts.RuleManifest{RuleType: "COLOR_PALETTE_COMPLIANCE", Version: "0.1.0", DisplayName: "ตรวจสีตาม Brand Palette", Engine: "go-image", Subject: "qc.check.color.v1", SupportedMIMETypes: []string{"image/png", "image/jpeg"}, DefaultSeverity: "MAJOR", TimeoutSec: 15, ParamSchema: map[string]any{"type": "object", "required": []string{"palette"}, "properties": map[string]any{"palette": map[string]any{"type": "array", "title": "สีที่อนุญาต (HEX)", "items": map[string]any{"type": "string", "pattern": "^#[0-9A-Fa-f]{6}$"}}, "deltaEThreshold": map[string]any{"type": "number", "title": "ค่าความต่างสีที่ยอมรับ (ใช้ RGB approximation ใน MVP)", "default": 30, "minimum": 0, "maximum": 255}}}}
}
func Dimension(request contracts.CheckTaskRequest) []contracts.Finding {
	var out []contracts.Finding
	if width, ok := Number(request.Rule.Params, "exactWidth"); ok && request.Specimen.WidthPx != int(width) {
		out = append(out, finding("DIMENSION_OUT_OF_RANGE", request.Rule.Severity, fmt.Sprintf("ความกว้างจริง %d px ต้องเท่ากับ %d px", request.Specimen.WidthPx, int(width))))
	}
	if height, ok := Number(request.Rule.Params, "exactHeight"); ok && request.Specimen.HeightPx != int(height) {
		out = append(out, finding("DIMENSION_OUT_OF_RANGE", request.Rule.Severity, fmt.Sprintf("ความสูงจริง %d px ต้องเท่ากับ %d px", request.Specimen.HeightPx, int(height))))
	}
	if expected, ok := Number(request.Rule.Params, "aspectRatio"); ok && request.Specimen.HeightPx > 0 {
		actual := float64(request.Specimen.WidthPx) / float64(request.Specimen.HeightPx)
		if math.Abs(actual-expected) > 0.015 {
			out = append(out, finding("ASPECT_RATIO_MISMATCH", request.Rule.Severity, fmt.Sprintf("อัตราส่วนจริง %.3f:1 ต้องเป็น %.3f:1", actual, expected)))
		}
	}
	dimensions := []struct {
		param    string
		actual   int
		tooSmall bool
		label    string
	}{{"minWidth", request.Specimen.WidthPx, true, "ความกว้าง"}, {"minHeight", request.Specimen.HeightPx, true, "ความสูง"}, {"maxWidth", request.Specimen.WidthPx, false, "ความกว้าง"}, {"maxHeight", request.Specimen.HeightPx, false, "ความสูง"}, {"minDPI", request.Specimen.DPI, true, "DPI"}}
	for _, d := range dimensions {
		limit, ok := Number(request.Rule.Params, d.param)
		if !ok {
			continue
		}
		bad := (d.tooSmall && float64(d.actual) < limit) || (!d.tooSmall && float64(d.actual) > limit)
		if bad {
			out = append(out, finding("DIMENSION_OUT_OF_RANGE", request.Rule.Severity, fmt.Sprintf("%s จริง %d ไม่ผ่านเงื่อนไข %s", d.label, d.actual, d.param)))
		}
	}
	return out
}
func FileSize(request contracts.CheckTaskRequest) []contracts.Finding {
	var out []contracts.Finding
	if max, ok := Number(request.Rule.Params, "maxSizeMB"); ok && request.Specimen.SizeBytes > int64(max*1024*1024) {
		out = append(out, finding("FILE_TOO_LARGE", request.Rule.Severity, fmt.Sprintf("ขนาดไฟล์ %d bytes เกิน %.2f MB", request.Specimen.SizeBytes, max)))
	}
	if min, ok := Number(request.Rule.Params, "minSizeKB"); ok && request.Specimen.SizeBytes < int64(min*1024) {
		out = append(out, finding("FILE_TOO_SMALL", request.Rule.Severity, fmt.Sprintf("ขนาดไฟล์ %d bytes ต่ำกว่า %.2f KB", request.Specimen.SizeBytes, min)))
	}
	allowed := StringList(request.Rule.Params, "allowedFormats")
	if len(allowed) > 0 {
		found := false
		for _, format := range allowed {
			if format == request.Specimen.Format {
				found = true
			}
		}
		if !found {
			out = append(out, finding("FORMAT_NOT_ALLOWED", request.Rule.Severity, "ชนิดไฟล์ "+request.Specimen.Format+" ไม่ได้รับอนุญาต"))
		}
	}
	return out
}

// Color samples actual uploaded pixels. The production replacement should use
// CIEDE2000 and colour-profile conversion; RGB distance is explicit MVP logic.
func Color(request contracts.CheckTaskRequest) []contracts.Finding {
	palette := StringList(request.Rule.Params, "palette")
	if len(palette) == 0 {
		return []contracts.Finding{finding("INVALID_PALETTE", request.Rule.Severity, "ต้องระบุ palette อย่างน้อยหนึ่งสี")}
	}
	colors := make([][3]uint8, 0, len(palette))
	for _, value := range palette {
		color, ok := parseHex(value)
		if ok {
			colors = append(colors, color)
		}
	}
	if len(colors) == 0 {
		return []contracts.Finding{finding("INVALID_PALETTE", request.Rule.Severity, "palette ต้องเป็น HEX เช่น #D0021B")}
	}
	threshold := 30.0
	if value, ok := Number(request.Rule.Params, "deltaEThreshold"); ok {
		threshold = value
	}
	key := strings.TrimPrefix(request.Specimen.ObjectKey, "local/")
	if key == "" || strings.Contains(key, "/") {
		return []contracts.Finding{finding("OBJECT_UNAVAILABLE", request.Rule.Severity, "ไม่พบไฟล์ภาพสำหรับตรวจสี")}
	}
	file, err := os.Open(filepath.Join(valueOr("OBJECT_ROOT", "/objects"), key))
	if err != nil {
		return []contracts.Finding{finding("OBJECT_UNAVAILABLE", request.Rule.Severity, "ไม่สามารถเปิดไฟล์ภาพที่อัปโหลด")}
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return []contracts.Finding{finding("UNSUPPORTED_IMAGE", request.Rule.Severity, "ไม่สามารถอ่านพิกเซลภาพ")}
	}
	bounds := img.Bounds()
	step := max(1, max(bounds.Dx(), bounds.Dy())/150)
	outside, total := 0, 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y += step {
		for x := bounds.Min.X; x < bounds.Max.X; x += step {
			r, g, b, _ := img.At(x, y).RGBA()
			if nearestDistance(uint8(r>>8), uint8(g>>8), uint8(b>>8), colors) > threshold {
				outside++
			}
			total++
		}
	}
	if total > 0 && float64(outside)/float64(total) > 0.02 {
		return []contracts.Finding{finding("OFF_PALETTE_COLOR", request.Rule.Severity, fmt.Sprintf("พบพิกเซลนอก Brand Palette ประมาณ %.1f%%", float64(outside)*100/float64(total)))}
	}
	return nil
}

func parseHex(value string) ([3]uint8, bool) {
	var out [3]uint8
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		number, err := strconv.ParseUint(value[i*2:i*2+2], 16, 8)
		if err != nil {
			return out, false
		}
		out[i] = uint8(number)
	}
	return out, true
}
func nearestDistance(r, g, b uint8, palette [][3]uint8) float64 {
	nearest := 100000.0
	for _, color := range palette {
		dr := float64(int(r) - int(color[0]))
		dg := float64(int(g) - int(color[1]))
		db := float64(int(b) - int(color[2]))
		distance := math.Sqrt(dr*dr + dg*dg + db*db)
		if distance < nearest {
			nearest = distance
		}
	}
	return nearest
}
func max(first, second int) int {
	if first > second {
		return first
	}
	return second
}
func valueOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func finding(defect, severity, message string) contracts.Finding {
	return contracts.Finding{ID: platform.NewID(), DefectType: defect, Severity: severity, Confidence: 1, Message: message}
}
