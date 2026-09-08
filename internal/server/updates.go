package server

import (
	"encoding/json"
	"io"
	"net/http"
)

func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	if s.Updates == nil {
		http.Error(w, "updates unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Path == "/api/v1/updates" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		writeJSON(w, http.StatusOK, s.Updates.Status())
		return
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	var err error
	switch r.URL.Path {
	case "/api/v1/updates/check":
		err = s.Updates.Check(r.Context())
	case "/api/v1/updates/open-directory":
		err = s.Updates.OpenDownloadDirectory()
	case "/api/v1/updates/preferences", "/api/v1/updates/install":
		var body struct {
			AutoCheck   *bool   `json:"auto_check"`
			Confirm     bool    `json:"confirm"`
			Version     string  `json:"version"`
			DownloadDir *string `json:"download_dir"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if decoder.Decode(&struct{}{}) != io.EOF {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/api/v1/updates/preferences" {
			if body.AutoCheck == nil && body.DownloadDir == nil {
				http.Error(w, "auto_check or download_dir is required", http.StatusBadRequest)
				return
			}
			err = s.Updates.SetPreferences(body.AutoCheck, body.DownloadDir)
		} else {
			if !body.Confirm || body.Version == "" {
				http.Error(w, "explicit version confirmation is required", http.StatusBadRequest)
				return
			}
			err = s.Updates.Install(body.Version)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, s.Updates.Status())
}
