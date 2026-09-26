package main

import "github.com/mam-ai-qc-checker/backend/internal/checker"

func main() {
	if err := checker.Run("checker-dimension", "qc.check.dimension.v1", checker.DimensionManifest(), checker.Dimension); err != nil {
		panic(err)
	}
}
