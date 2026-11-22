package web

import (
	"encoding/hex"
	"io/fs"
	"strings"
)

const (
	imgen40WorkerPath = "/static/js/imgen40-worker.js"
	imgen40ModelFile  = "models/imgen40/generator_fp16.onnx"
	imgen40ModelPath  = "/static/" + imgen40ModelFile
)

// imgen40Model returns an empty version when a local checkout has only the previews.
func imgen40Model(staticFS fs.FS) (string, int64) {
	file, err := fs.Stat(staticFS, imgen40ModelFile)
	if err != nil || !file.Mode().IsRegular() || file.Size() == 0 {
		return "", 0
	}
	manifest, err := fs.ReadFile(staticFS, "models/imgen40/SHA256SUMS")
	if err != nil {
		return "", 0
	}
	fields := strings.Fields(string(manifest))
	if len(fields) != 2 || len(fields[0]) != 64 || fields[1] != "static/"+imgen40ModelFile {
		return "", 0
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", 0
	}
	return fields[0][:12], file.Size()
}
