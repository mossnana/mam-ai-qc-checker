package main

import "github.com/mam-ai-qc-checker/backend/internal/checker"

func main() {
	if err := checker.Run("checker-filesize", "qc.check.filesize.v1", checker.FileSizeManifest(), checker.FileSize); err != nil {
		panic(err)
	}
}
