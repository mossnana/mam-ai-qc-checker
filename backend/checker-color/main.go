package main

import "github.com/mam-ai-qc-checker/backend/internal/checker"

func main() {
	if err := checker.Run("checker-color", "qc.check.color.v1", checker.ColorManifest(), checker.Color); err != nil {
		panic(err)
	}
}
