package server

import (
	"embed"
	"html/template"
)

var (
	//go:embed templates/index.html
	templateFS embed.FS

	indexTemplate = template.Must(template.ParseFS(templateFS, "templates/index.html"))
)

type indexPageData struct {
	DefaultAmount    int
	SuccessOverlayMs int
}
