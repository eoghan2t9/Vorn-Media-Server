package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

const backupTimeout = 10 * time.Minute

// handleDownloadBackup streams a plain-SQL pg_dump of the whole database
// straight through to the client. Everything Vorn stores -- users,
// libraries, media items, and every admin setting/credential (TMDb key,
// debrid accounts, Usenet servers, ...) -- lives in this one Postgres
// instance via the server_settings/dedicated-table pattern, so a single
// dump is a genuinely complete backup, not just "the media library part"
// of one. That also means the downloaded file is as sensitive as the live
// database itself (it contains those credentials in plain text) and
// should be stored/transferred with the same care.
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		writeError(w, http.StatusServiceUnavailable, "pg_dump is not installed on this server (the Docker image includes it; a native install needs the postgresql-client package)")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), backupTimeout)
	defer cancel()

	// --clean --if-exists prefixes every object with a DROP ... IF EXISTS,
	// so restoring lands cleanly on top of an already-populated database
	// (the normal case -- restoring is how you replace what's there, not
	// just how you seed an empty one) instead of erroring on "relation
	// already exists" for every table.
	cmd := exec.CommandContext(ctx, "pg_dump", "--no-owner", "--no-privileges", "--clean", "--if-exists", s.postgresDSN)
	cmd.Stderr = os.Stderr // pg_dump's own warnings (e.g. client/server version mismatch) land in the container's logs

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "starting backup")
		return
	}
	if err := cmd.Start(); err != nil {
		writeError(w, http.StatusInternalServerError, "starting backup")
		return
	}

	filename := fmt.Sprintf("vorn-backup-%s.sql", time.Now().UTC().Format("20060102-150405"))
	w.Header().Set("Content-Type", "application/sql")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))

	// Streamed directly rather than buffered, so a large database doesn't
	// need to fit in memory -- but that also means the response has
	// already started by the time a failure could happen, so a pg_dump
	// error partway through can only be logged server-side, not turned
	// into an HTTP error status (the 200 + headers are already sent).
	if _, err := io.Copy(w, stdout); err != nil {
		log.Printf("httpapi: streaming backup: %v", err)
		_ = cmd.Process.Kill()
		return
	}
	if err := cmd.Wait(); err != nil {
		log.Printf("httpapi: pg_dump exited with error: %v", err)
	}
}

// handleRestoreBackup replaces the entire database with a previously
// downloaded backup uploaded in the request body. This is deliberately
// destructive -- the frontend must get explicit confirmation before ever
// calling this.
func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if _, err := exec.LookPath("psql"); err != nil {
		writeError(w, http.StatusServiceUnavailable, "psql is not installed on this server (the Docker image includes it; a native install needs the postgresql-client package)")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), backupTimeout)
	defer cancel()

	if err := s.restoreFrom(ctx, io.LimitReader(r.Body, 1<<30)); err != nil { // 1GiB cap -- generous for a metadata DB, just a backstop
		writeError(w, http.StatusBadRequest, "restore failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "restore completed, server is restarting"})
	scheduleRestartAfterRestore()
}

// restoreFrom pipes sqlSource into psql, replacing the entire database.
// Wrapped in a single transaction (--single-transaction) so a corrupt/
// partial source rolls back cleanly instead of leaving the database
// half-restored. Shared by handleRestoreBackup (an uploaded file) and
// handleRestoreAutoBackup (an on-disk automated backup, admin_backups.go).
func (s *Server) restoreFrom(ctx context.Context, sqlSource io.Reader) error {
	cmd := exec.CommandContext(ctx, "psql", "--single-transaction", "-v", "ON_ERROR_STOP=1", s.postgresDSN, "-f", "-")
	cmd.Stdin = sqlSource
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New(strings.TrimSpace(stderr.String()))
	}
	return nil
}

// scheduleRestartAfterRestore mirrors handleRestartServer's "exit and let
// the platform restart me" contract -- after a full database swap, every
// in-memory session/cache and the migrate.Up() version table need a
// genuinely fresh process, not just a fresh Postgres connection.
func scheduleRestartAfterRestore() {
	go func() {
		time.Sleep(300 * time.Millisecond)
		os.Exit(0)
	}()
}
