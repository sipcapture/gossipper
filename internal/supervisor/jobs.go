// Package supervisor manages the lifecycle of UI-launched gossipper engines.
//
// In Phase 0 the supervisor exposes only a persistence layer (JobsStore) plus a
// process-less Registry stub. Phase 2 swaps the stub for a real fork/exec
// launcher that wires worker processes back to the master via stdout JSON
// lines.
package supervisor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Status enumerates job lifecycle states.
type Status string

const (
	// StatusPending — created in DB but worker not started yet.
	StatusPending Status = "pending"
	// StatusRunning — worker started and is processing.
	StatusRunning Status = "running"
	// StatusSucceeded — worker exited normally with exit code 0.
	StatusSucceeded Status = "succeeded"
	// StatusFailed — worker exited with a non-zero code or supervisor error.
	StatusFailed Status = "failed"
	// StatusStopped — operator-requested stop.
	StatusStopped Status = "stopped"
)

// Job is a persisted representation of a worker run.
type Job struct {
	ID           string    `json:"id"`
	ProfileID    string    `json:"profile_id,omitempty"`
	ProfileKind  string    `json:"profile_kind,omitempty"`
	ScenarioID   string    `json:"scenario_id,omitempty"`
	Status       Status    `json:"status"`
	ArgsJSON     string    `json:"args_json,omitempty"`
	ArtifactsDir string    `json:"artifacts_dir,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	Error        string    `json:"error,omitempty"`
	CreatedBy    *int64    `json:"created_by,omitempty"`
	PID          *int      `json:"pid,omitempty"`
}

// JobsStore persists Job rows in the settings SQLite DB.
type JobsStore struct {
	db *sql.DB
}

// NewJobsStore wraps an existing settings DB (settingsauth.OpenStore output).
func NewJobsStore(db *sql.DB) *JobsStore {
	return &JobsStore{db: db}
}

// Create inserts a new pending job. Caller should set ID; CreatedAt defaults to now.
func (s *JobsStore) Create(ctx context.Context, j Job) (Job, error) {
	id := strings.TrimSpace(j.ID)
	if id == "" {
		return Job{}, errors.New("supervisor: job id is required")
	}
	if j.Status == "" {
		j.Status = StatusPending
	}
	if j.CreatedAt.IsZero() {
		j.CreatedAt = time.Now().UTC()
	}
	if j.ArgsJSON == "" {
		j.ArgsJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO jobs (id, profile_id, profile_kind, scenario_id, status, args_json, artifacts_dir, created_at, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullableStr(j.ProfileID), j.ProfileKind, nullableStr(j.ScenarioID),
		string(j.Status), j.ArgsJSON, j.ArtifactsDir, j.CreatedAt.UTC().Format(time.RFC3339Nano), nullableInt64(j.CreatedBy))
	if err != nil {
		return Job{}, fmt.Errorf("supervisor: insert job: %w", err)
	}
	return s.Get(ctx, id)
}

// UpdateStatus sets job.status (and finished_at / exit_code / error when applicable).
func (s *JobsStore) UpdateStatus(ctx context.Context, id string, status Status, exitCode *int, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	switch status {
	case StatusRunning:
		_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, started_at=COALESCE(started_at, ?) WHERE id=?`,
			string(status), now, id)
		return err
	case StatusSucceeded, StatusFailed, StatusStopped:
		_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?, finished_at=?, exit_code=?, error=? WHERE id=?`,
			string(status), now, nullableInt(exitCode), strings.TrimSpace(errMsg), id)
		return err
	default:
		_, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=? WHERE id=?`, string(status), id)
		return err
	}
}

// SetPID attaches a worker PID to a job (used once exec succeeds).
func (s *JobsStore) SetPID(ctx context.Context, id string, pid int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET pid=? WHERE id=?`, pid, id)
	return err
}

// Get returns a single job by id.
func (s *JobsStore) Get(ctx context.Context, id string) (Job, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, profile_id, profile_kind, scenario_id, status, args_json, artifacts_dir,
       created_at, started_at, finished_at, exit_code, error, created_by, pid
FROM jobs WHERE id = ?`, id)
	return scanJob(row)
}

// List returns the most recent jobs first (LIMIT defaults to 100 when <= 0).
func (s *JobsStore) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, profile_id, profile_kind, scenario_id, status, args_json, artifacts_dir,
       created_at, started_at, finished_at, exit_code, error, created_by, pid
FROM jobs ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Delete removes a job (and its artifacts row via FK cascade).
func (s *JobsStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddArtifact records a file produced by a job.
func (s *JobsStore) AddArtifact(ctx context.Context, jobID, kind, path string, size int64) error {
	if strings.TrimSpace(jobID) == "" || strings.TrimSpace(kind) == "" || strings.TrimSpace(path) == "" {
		return errors.New("supervisor: AddArtifact requires job_id, kind, path")
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO job_artifacts (job_id, kind, path, size_bytes, created_at)
VALUES (?, ?, ?, ?, ?)`,
		jobID, kind, path, size, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Artifact represents a single file produced by a job.
type Artifact struct {
	ID        int64     `json:"id"`
	JobID     string    `json:"job_id"`
	Kind      string    `json:"kind"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// ListArtifacts returns artifacts for a job ordered by creation time asc.
func (s *JobsStore) ListArtifacts(ctx context.Context, jobID string) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, job_id, kind, path, size_bytes, created_at FROM job_artifacts
WHERE job_id = ? ORDER BY created_at ASC, id ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		var created string
		if err := rows.Scan(&a.ID, &a.JobID, &a.Kind, &a.Path, &a.SizeBytes, &created); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
			a.CreatedAt = t.UTC()
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReportRow is a summary/report artifact with its parent job metadata.
type ReportRow struct {
	JobID       string    `json:"job_id"`
	ProfileKind string    `json:"profile_kind,omitempty"`
	ProfileID   string    `json:"profile_id,omitempty"`
	JobStatus   string    `json:"job_status"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
	Artifact    Artifact  `json:"artifact"`
}

// ListReportArtifacts returns summary/HTML/PDF artifacts across jobs, newest first.
func (s *JobsStore) ListReportArtifacts(ctx context.Context, limit int) ([]ReportRow, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT j.id, j.profile_kind, j.profile_id, j.status, j.finished_at,
       a.id, a.job_id, a.kind, a.path, a.size_bytes, a.created_at
FROM job_artifacts a
JOIN jobs j ON j.id = a.job_id
WHERE a.kind IN ('summary', 'report_html', 'report_pdf')
ORDER BY a.created_at DESC, a.id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportRow
	for rows.Next() {
		var (
			row        ReportRow
			profileKind sql.NullString
			profileID   sql.NullString
			finished    sql.NullString
			created     string
		)
		if err := rows.Scan(
			&row.JobID, &profileKind, &profileID, &row.JobStatus, &finished,
			&row.Artifact.ID, &row.Artifact.JobID, &row.Artifact.Kind, &row.Artifact.Path, &row.Artifact.SizeBytes, &created,
		); err != nil {
			return nil, err
		}
		row.ProfileKind = profileKind.String
		row.ProfileID = profileID.String
		if finished.Valid && finished.String != "" {
			if t, err := time.Parse(time.RFC3339Nano, finished.String); err == nil {
				row.FinishedAt = t.UTC()
			}
		}
		if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
			row.Artifact.CreatedAt = t.UTC()
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ErrNotFound is returned when an entity (job, artifact) does not exist.
var ErrNotFound = errors.New("supervisor: not found")

// scanJob accepts both *sql.Row and *sql.Rows.
func scanJob(scanner interface {
	Scan(dest ...any) error
}) (Job, error) {
	var (
		j           Job
		profileID   sql.NullString
		scenarioID  sql.NullString
		startedAt   sql.NullString
		finishedAt  sql.NullString
		exitCode    sql.NullInt64
		errStr      sql.NullString
		createdBy   sql.NullInt64
		pid         sql.NullInt64
		createdAt   string
		status      string
		profileKind sql.NullString
		artifacts   sql.NullString
		argsJSON    sql.NullString
	)
	if err := scanner.Scan(&j.ID, &profileID, &profileKind, &scenarioID, &status, &argsJSON, &artifacts,
		&createdAt, &startedAt, &finishedAt, &exitCode, &errStr, &createdBy, &pid); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Job{}, ErrNotFound
		}
		return Job{}, err
	}
	j.ProfileID = profileID.String
	j.ProfileKind = profileKind.String
	j.ScenarioID = scenarioID.String
	j.Status = Status(status)
	j.ArgsJSON = argsJSON.String
	if j.ArgsJSON == "" {
		j.ArgsJSON = "{}"
	}
	j.ArtifactsDir = artifacts.String
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		j.CreatedAt = t.UTC()
	}
	if startedAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, startedAt.String); err == nil {
			tt := t.UTC()
			j.StartedAt = &tt
		}
	}
	if finishedAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, finishedAt.String); err == nil {
			tt := t.UTC()
			j.FinishedAt = &tt
		}
	}
	if exitCode.Valid {
		v := int(exitCode.Int64)
		j.ExitCode = &v
	}
	if errStr.Valid {
		j.Error = errStr.String
	}
	if createdBy.Valid {
		v := createdBy.Int64
		j.CreatedBy = &v
	}
	if pid.Valid {
		v := int(pid.Int64)
		j.PID = &v
	}
	return j, nil
}

func nullableStr(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func nullableInt64(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

// EncodeArgs is a helper for callers that need to store opaque JSON args.
func EncodeArgs(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
