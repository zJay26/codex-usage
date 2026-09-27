package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

//go:embed static/*
var assets embed.FS

func Handler() http.Handler {
	sub, _ := fs.Sub(assets, "static")
	files := http.FileServer(http.FS(sub))
	index, _ := fs.ReadFile(sub, "index.html")
	for _, name := range []string{"styles.css", "i18n.js", "app.js", "updates.js", "icon.svg", "favicon-32.png", "apple-touch-icon.png"} {
		content, _ := fs.ReadFile(sub, name)
		index = bytes.ReplaceAll(index, []byte(`"/`+name+`"`),
			[]byte(`"/`+name+`?v=`+assetVersion(content)+`"`))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(sub, path); err != nil {
			path = "index.html"
		}
		if path == "index.html" {
			w.Header().Set("Cache-Control", "no-store")
			http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
			return
		} else {
			// The index points at content-derived URLs, so a binary upgrade always
			// receives matching scripts, styles and icons even when Edge caches assets.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + path
		files.ServeHTTP(w, r2)
	})
}

func assetVersion(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:6])
}
