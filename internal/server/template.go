package server

import (
	"embed"
	htmltemplate "html/template"
	"io/fs"
	"text/template"
)

// AppVersion is set at build time via ldflags
var AppVersion = "dev"

var (
	//go:embed templates/index.html templates/main.js templates/api.js templates/ui.js templates/qr.js
	templateFS embed.FS

	indexTemplate = htmltemplate.Must(htmltemplate.ParseFS(templateFS, "templates/index.html"))
	mainJS        = template.Must(template.ParseFS(templateFS, "templates/main.js"))
)

type indexPageData struct {
	DefaultAmount    int
	SuccessOverlayMs int
	Version          string
}

func GetStaticFile(filename string) ([]byte, error) {
	return fs.ReadFile(templateFS, "templates/"+filename)
}
