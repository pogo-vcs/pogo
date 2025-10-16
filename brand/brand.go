package brand

import (
	_ "embed"
	"net/http"
)

//go:embed logo.svg
var Logo []byte

//go:embed logo.png
var LogoPng []byte

func LogoHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=3600") // Cache for 1 hour
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(Logo)
}
