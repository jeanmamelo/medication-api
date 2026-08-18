// Package swaggerui embeds a vendored Swagger UI static build so the API can serve
// interactive OpenAPI documentation without any external CDN or network dependency at
// runtime. Assets are vendored from swagger-ui-dist (Apache-2.0); see dist/LICENSE and
// dist/NOTICE.
package swaggerui

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

// FS serves the Swagger UI static files with "dist" stripped, so paths match what the
// bundled index.html requests (e.g. "swagger-ui-bundle.js" instead of "dist/swagger-ui-bundle.js").
var FS = mustSub(embedded, "dist")

func mustSub(fsys embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
