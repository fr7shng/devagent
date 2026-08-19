package sidecar

import (
	"testing"
)

func TestProgressTracker_RegisterAndGet(t *testing.T) {
	pt := NewProgressTracker()
	pt.Register("job_1", "req_1", "shelf_01")
	job, ok := pt.GetJob("job_1")
	if !ok {
		t.Fatal("expected job to exist")
	}
	if job.RequestID != "req_1" {
		t.Errorf("expected request_id 'req_1', got '%s'", job.RequestID)
	}
}

func TestProgressTracker_Complete(t *testing.T) {
	pt := NewProgressTracker()
	pt.Register("job_1", "req_1", "shelf_01")
	pt.Complete("job_1", map[string]any{"pin": 1, "state": true})
	job, _ := pt.GetJob("job_1")
	if job.Status != "completed" {
		t.Errorf("expected status 'completed', got '%s'", job.Status)
	}
}

func TestProgressTracker_UpdateProgress(t *testing.T) {
	pt := NewProgressTracker()
	pt.Register("job_1", "req_1", "shelf_01")
	pt.UpdateProgress("job_1", 50, "pending")
	job, _ := pt.GetJob("job_1")
	if job.Progress != 50 {
		t.Errorf("expected progress 50, got %d", job.Progress)
	}
}
