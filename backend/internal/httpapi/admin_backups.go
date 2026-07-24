package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/eoghan2t9/vorn-media-server/backend/internal/backup"
	"github.com/eoghan2t9/vorn-media-server/backend/internal/store"
)

// This file manages the automated, on-disk backup schedule (see
// internal/backup) -- distinct from admin_backup.go's manual, never-
// touches-disk download/restore-from-upload.

type backupSettingsResponse struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
}

func (s *Server) handleGetBackupSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetBackupSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "loading backup settings")
		return
	}
	writeJSON(w, http.StatusOK, backupSettingsResponse{Enabled: settings.Enabled, IntervalHours: settings.IntervalHours})
}

type updateBackupSettingsRequest struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"intervalHours"`
}

func (s *Server) handleUpdateBackupSettings(w http.ResponseWriter, r *http.Request) {
	var req updateBackupSettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IntervalHours < 1 {
		writeError(w, http.StatusBadRequest, "intervalHours must be at least 1")
		return
	}
	if err := s.store.SetBackupSettings(store.BackupSettings{Enabled: req.Enabled, IntervalHours: req.IntervalHours}); err != nil {
		writeError(w, http.StatusInternalServerError, "saving backup settings")
		return
	}
	s.handleGetBackupSettings(w, r)
}

type autoBackupResponse struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"sizeBytes"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleListAutoBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := backup.List(s.backupDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "listing backups")
		return
	}
	resp := make([]autoBackupResponse, 0, len(backups))
	for _, b := range backups {
		resp = append(resp, autoBackupResponse{Filename: b.Filename, SizeBytes: b.SizeBytes, CreatedAt: b.CreatedAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDownloadAutoBackup and the two handlers below all take the backup
// filename from the URL path -- backup.ValidFilename's strict pattern
// match is what rules out path traversal before ever joining it onto
// backupDir, not filepath.Clean alone.

func (s *Server) handleDownloadAutoBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !backup.ValidFilename(filename) {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	f, err := os.Open(filepath.Join(s.backupDir, filename))
	if err != nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	io.Copy(w, f)
}

func (s *Server) handleDeleteAutoBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !backup.ValidFilename(filename) {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	if err := os.Remove(filepath.Join(s.backupDir, filename)); err != nil {
		if os.IsNotExist(err) {
			writeError(w, http.StatusNotFound, "backup not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "deleting backup")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRestoreAutoBackup is the same destructive replace-the-whole-
// database operation as handleRestoreBackup, just sourced from an on-disk
// automated backup instead of an upload.
func (s *Server) handleRestoreAutoBackup(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("filename")
	if !backup.ValidFilename(filename) {
		writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	if _, err := exec.LookPath("psql"); err != nil {
		writeError(w, http.StatusServiceUnavailable, "psql is not installed on this server (the Docker image includes it; a native install needs the postgresql-client package)")
		return
	}
	f, err := os.Open(filepath.Join(s.backupDir, filename))
	if err != nil {
		writeError(w, http.StatusNotFound, "backup not found")
		return
	}
	defer f.Close()

	ctx, cancel := context.WithTimeout(r.Context(), backupTimeout)
	defer cancel()

	if err := s.restoreFrom(ctx, f); err != nil {
		writeError(w, http.StatusBadRequest, "restore failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "restore completed, server is restarting"})
	scheduleRestartAfterRestore()
}
