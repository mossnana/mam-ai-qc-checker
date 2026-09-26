package checker

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/mam-ai-qc-checker/backend/internal/contracts"
)

func TestColorChecksUploadedPixels(t *testing.T) {
	root := t.TempDir()
	t.Setenv("OBJECT_ROOT", root)
	file, err := os.Create(filepath.Join(root, "red.png"))
	if err != nil {
		t.Fatal(err)
	}
	imageData := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			imageData.Set(x, y, color.NRGBA{R: 210, G: 2, B: 27, A: 255})
		}
	}
	if err := png.Encode(file, imageData); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	request := contracts.CheckTaskRequest{Specimen: contracts.SpecimenRef{ObjectKey: "local/red.png"}, Rule: contracts.RuleSnapshot{Severity: "MAJOR", Params: map[string]any{"palette": []any{"#D2021B"}, "deltaEThreshold": 5.0}}}
	if findings := Color(request); len(findings) != 0 {
		t.Fatalf("expected palette match, got %#v", findings)
	}
	request.Rule.Params["palette"] = []any{"#00FF00"}
	if findings := Color(request); len(findings) != 1 || findings[0].DefectType != "OFF_PALETTE_COLOR" {
		t.Fatalf("expected off-palette finding, got %#v", findings)
	}
}

func TestDimensionChecksExactSizeAndAspectRatio(t *testing.T) {
	request := contracts.CheckTaskRequest{Specimen: contracts.SpecimenRef{WidthPx: 1080, HeightPx: 1080}, Rule: contracts.RuleSnapshot{Severity: "MAJOR", Params: map[string]any{"exactWidth": 1080.0, "exactHeight": 1350.0, "aspectRatio": 0.8}}}
	findings := Dimension(request)
	if len(findings) != 2 {
		t.Fatalf("expected exact-height and aspect-ratio findings, got %#v", findings)
	}
	if findings[0].DefectType != "DIMENSION_OUT_OF_RANGE" || findings[1].DefectType != "ASPECT_RATIO_MISMATCH" {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}
