package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CronJob represents a scheduled recurring or one-shot task.
type CronJob struct {
	ID        string    `json:"id"`
	Schedule  string    `json:"schedule"`
	Prompt    string    `json:"prompt"`
	Recurring bool      `json:"recurring"`
	Durable   bool      `json:"durable"`
	CreatedAt time.Time `json:"createdAt"`
	LastRun   time.Time `json:"lastRun,omitempty"`
	NextRun   time.Time `json:"nextRun"`
	Runs      int       `json:"runs"`
	// MaxRuns caps how many times a recurring job may fire (0 = unlimited).
	MaxRuns int `json:"maxRuns,omitempty"`
	// ExpiresAt auto-deletes the job after this time (zero = never).
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	cancel    context.CancelFunc
}

// CronScheduler manages scheduled tasks.
type CronScheduler struct {
	mu   sync.RWMutex
	jobs map[string]*CronJob
	next int
}

// maxCronJobs caps the number of concurrently scheduled jobs so an LLM
// cannot create unbounded jobs in a tight loop (each Create stores an entry
// in the in-memory map).
const maxCronJobs = 256

var globalCronScheduler = &CronScheduler{jobs: make(map[string]*CronJob)}

func GetCronScheduler() *CronScheduler { return globalCronScheduler }

func (s *CronScheduler) Create(schedule, prompt string, recurring, durable bool) (*CronJob, error) {
	return s.CreateWithLimits(schedule, prompt, recurring, durable, 0, time.Time{})
}

// CreateWithLimits creates a job with optional maxRuns and expiresAt (PACK-06 /loop).
func (s *CronScheduler) CreateWithLimits(schedule, prompt string, recurring, durable bool, maxRuns int, expiresAt time.Time) (*CronJob, error) {
	nextRun, err := nextCronTime(schedule)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", schedule, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.jobs) >= maxCronJobs {
		return nil, fmt.Errorf("cron job limit reached (%d); delete existing jobs first", maxCronJobs)
	}
	s.next++
	id := fmt.Sprintf("cron_%d", s.next)

	ctx, cancel := context.WithCancel(context.Background())
	_ = ctx

	job := &CronJob{
		ID:        id,
		Schedule:  schedule,
		Prompt:    prompt,
		Recurring: recurring,
		Durable:   durable,
		CreatedAt: time.Now(),
		NextRun:   nextRun,
		MaxRuns:   maxRuns,
		ExpiresAt: expiresAt,
		cancel:    cancel,
	}
	s.jobs[id] = job
	return job, nil
}

// TickRun records a successful fire; returns true if the job should be deleted
// (max runs or expiry reached).
func (s *CronScheduler) TickRun(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return true
	}
	job.Runs++
	job.LastRun = time.Now()
	if !job.ExpiresAt.IsZero() && time.Now().After(job.ExpiresAt) {
		if job.cancel != nil {
			job.cancel()
		}
		delete(s.jobs, id)
		return true
	}
	if job.MaxRuns > 0 && job.Runs >= job.MaxRuns {
		if job.cancel != nil {
			job.cancel()
		}
		delete(s.jobs, id)
		return true
	}
	if !job.Recurring {
		if job.cancel != nil {
			job.cancel()
		}
		delete(s.jobs, id)
		return true
	}
	if next, err := nextCronTime(job.Schedule); err == nil {
		job.NextRun = next
	}
	return false
}

func (s *CronScheduler) List() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, j)
	}
	return out
}

func (s *CronScheduler) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[id]
	if !ok {
		return false
	}
	if job.cancel != nil {
		job.cancel()
	}
	delete(s.jobs, id)
	return true
}

func (s *CronScheduler) Get(id string) (*CronJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	return j, ok
}

// nextCronTime parses a 5-field cron expression and finds the next matching time.
func nextCronTime(expr string) (time.Time, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return time.Time{}, fmt.Errorf("expected 5 fields, got %d", len(fields))
	}

	now := time.Now()
	// Simple implementation: try each minute for the next 48 hours
	candidate := now.Truncate(time.Minute).Add(time.Minute)
	limit := now.Add(48 * time.Hour)
	for candidate.Before(limit) {
		if cronMatches(fields, candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no match found in next 48 hours for %q", expr)
}

func cronMatches(fields []string, t time.Time) bool {
	return fieldMatches(fields[0], t.Minute()) &&
		fieldMatches(fields[1], t.Hour()) &&
		fieldMatches(fields[2], t.Day()) &&
		fieldMatches(fields[3], int(t.Month())) &&
		fieldMatches(fields[4], int(t.Weekday()))
}

func fieldMatches(field string, value int) bool {
	if field == "*" {
		return true
	}
	// Handle */N step values
	if strings.HasPrefix(field, "*/") {
		step, err := strconv.Atoi(field[2:])
		if err != nil || step <= 0 {
			return false
		}
		return value%step == 0
	}
	// Handle comma-separated values
	for _, part := range strings.Split(field, ",") {
		// Handle ranges
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(bounds[0])
			hi, err2 := strconv.Atoi(bounds[1])
			if err1 == nil && err2 == nil && value >= lo && value <= hi {
				return true
			}
			continue
		}
		n, err := strconv.Atoi(part)
		if err == nil && n == value {
			return true
		}
	}
	return false
}

// CronCreateTool schedules a prompt to run on a cron schedule.
type CronCreateTool struct{}

func (CronCreateTool) Name() string      { return "CronCreate" }
func (CronCreateTool) Aliases() []string { return []string{"cron_create", "ScheduleWakeup"} }
func (CronCreateTool) Description() string {
	return "Schedule a prompt to run at a future time — either recurring on a cron schedule, or once at a specific time."
}

// CronCreateInput is the typed input for CronCreateTool.
type CronCreateInput struct {
	Schedule     string `json:"schedule"`
	Prompt       string `json:"prompt"`
	Recurring    *bool  `json:"recurring"`
	Durable      *bool  `json:"durable"`
	MaxRuns      int    `json:"max_runs"`
	ExpiresInSec int    `json:"expires_in_sec"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CronCreateTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"schedule":       {Type: "string", Description: "5-field cron expression in user's local timezone: minute hour day-of-month month day-of-week"},
			"prompt":         {Type: "string", Description: "The prompt to enqueue when the schedule fires"},
			"recurring":      {Type: "boolean", Description: "If true, repeats on schedule. If false, fires once then auto-deletes (default: true)"},
			"durable":        {Type: "boolean", Description: "If true, persists to disk and survives session restarts (default: false)"},
			"max_runs":       {Type: "integer", Description: "Stop after this many fires (0 = unlimited). Useful for /loop-style caps."},
			"expires_in_sec": {Type: "integer", Description: "Auto-delete job after this many seconds from creation (0 = never)."},
		},
		Required: []string{"schedule", "prompt"},
	}
}

func (CronCreateTool) Parameters() map[string]interface{} {
	return cronCreateSchema.ToJSONSchema()
}

// cronCreateSchema is the single source of truth for CronCreate's input schema.
var cronCreateSchema = CronCreateTool{}.Schema()

func (CronCreateTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[CronCreateInput]("CronCreate", input)
	if err != nil {
		return "", err
	}
	if p.Schedule == "" {
		return "", fmt.Errorf("schedule is required")
	}
	if p.Prompt == "" {
		return "", fmt.Errorf("prompt is required")
	}

	recurring := true
	if p.Recurring != nil {
		recurring = *p.Recurring
	}
	durable := false
	if p.Durable != nil {
		durable = *p.Durable
	}
	var expires time.Time
	if p.ExpiresInSec > 0 {
		expires = time.Now().Add(time.Duration(p.ExpiresInSec) * time.Second)
	}
	if p.MaxRuns < 0 {
		p.MaxRuns = 0
	}

	job, err := globalCronScheduler.CreateWithLimits(p.Schedule, p.Prompt, recurring, durable, p.MaxRuns, expires)
	if err != nil {
		return "", err
	}

	out, _ := json.Marshal(map[string]any{
		"id":       job.ID,
		"schedule": job.Schedule,
		"nextRun":  job.NextRun.Format(time.RFC3339),
		"type":     map[bool]string{true: "recurring", false: "one-shot"}[recurring],
		"maxRuns":  job.MaxRuns,
		"expires":  formatOptionalTime(job.ExpiresAt),
	})
	return string(out), nil
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// CronDeleteTool removes a scheduled job.
type CronDeleteTool struct{}

func (CronDeleteTool) Name() string        { return "CronDelete" }
func (CronDeleteTool) Aliases() []string   { return []string{"cron_delete"} }
func (CronDeleteTool) Description() string { return "Remove a scheduled cron job" }

// CronDeleteInput is the typed input for CronDeleteTool.
type CronDeleteInput struct {
	ID string `json:"id"`
}

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CronDeleteTool) Schema() ToolSchema {
	return ToolSchema{
		Type: "object",
		Properties: map[string]SchemaProperty{
			"id": {Type: "string", Description: "The cron job ID to delete"},
		},
		Required: []string{"id"},
	}
}

func (CronDeleteTool) Parameters() map[string]interface{} {
	return cronDeleteSchema.ToJSONSchema()
}

// cronDeleteSchema is the single source of truth for CronDelete's input schema.
var cronDeleteSchema = CronDeleteTool{}.Schema()

func (CronDeleteTool) Execute(_ context.Context, input json.RawMessage) (string, error) {
	p, err := DecodeInput[CronDeleteInput]("CronDelete", input)
	if err != nil {
		return "", err
	}
	if !globalCronScheduler.Delete(p.ID) {
		return "", fmt.Errorf("cron job %q not found", p.ID)
	}
	return fmt.Sprintf("Deleted cron job %s", p.ID), nil
}

// CronListTool lists all scheduled jobs.
type CronListTool struct{}

func (CronListTool) Name() string        { return "CronList" }
func (CronListTool) Aliases() []string   { return []string{"cron_list"} }
func (CronListTool) Description() string { return "List all scheduled cron jobs" }

// Schema returns the typed input schema. Parameters() delegates to it so the
// two cannot diverge.
func (CronListTool) Schema() ToolSchema {
	return ToolSchema{Type: "object", Properties: map[string]SchemaProperty{}}
}

func (CronListTool) Parameters() map[string]interface{} {
	return cronListSchema.ToJSONSchema()
}

// cronListSchema is the single source of truth for CronList's input schema.
var cronListSchema = CronListTool{}.Schema()

func (CronListTool) Execute(_ context.Context, _ json.RawMessage) (string, error) {
	jobs := globalCronScheduler.List()
	items := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, map[string]any{
			"id":        j.ID,
			"schedule":  j.Schedule,
			"prompt":    j.Prompt,
			"recurring": j.Recurring,
			"nextRun":   j.NextRun.Format(time.RFC3339),
			"runs":      j.Runs,
		})
	}
	out, _ := json.Marshal(map[string]any{"jobs": items})
	return string(out), nil
}
