package server

import (
	"embed"
	"html/template"
)

var (
	//go:embed templates/index.html templates/main.js templates/api.js templates/ui.js templates/qr.js
	templateFS embed.FS

	indexTemplate = template.Must(template.ParseFS(templateFS, "templates/index.html", "templates/main.js", "templates/api.js", "templates/ui.js", "templates/qr.js"))
)

type indexPageData struct {
	DefaultAmount    int
	SuccessOverlayMs int
}
