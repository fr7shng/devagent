package sidecar

import (
	"context"
	"sync"
	"time"

	"github.com/ng/devagent/internal/model"
)

const jobTTL = 10 * time.Minute

type ProgressTracker struct {
	jobs map[string]*model.Job
	mu   sync.RWMutex
}

func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		jobs: make(map[string]*model.Job),
	}
}

func (pt *ProgressTracker) StartCleanup(ctx context.Context) {
	go pt.cleanup(ctx)
}

func (pt *ProgressTracker) Register(jobID, requestID, deviceID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.jobs[jobID] = &model.Job{
		ID:        jobID,
		RequestID: requestID,
		DeviceID:  deviceID,
		Status:    "pending",
		Progress:  0,
		CreatedAt: time.Now(),
	}
}

func (pt *ProgressTracker) GetJob(jobID string) (*model.Job, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	job, ok := pt.jobs[jobID]
	if !ok {
		return nil, false
	}
	return job, true
}

func (pt *ProgressTracker) UpdateProgress(jobID string, progress int, status, message string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if job, ok := pt.jobs[jobID]; ok {
		job.Progress = progress
		job.Status = status
	}
}

func (pt *ProgressTracker) Complete(jobID string, result any) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if job, ok := pt.jobs[jobID]; ok {
		job.Status = "completed"
		job.Progress = 100
		job.Result = result
	}
}

func (pt *ProgressTracker) Fail(jobID string, errMsg string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if job, ok := pt.jobs[jobID]; ok {
		job.Status = "error"
		job.Error = errMsg
	}
}

func (pt *ProgressTracker) cleanup(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pt.mu.Lock()
			now := time.Now()
			for id, job := range pt.jobs {
				if now.Sub(job.CreatedAt) > jobTTL {
					delete(pt.jobs, id)
				}
			}
			pt.mu.Unlock()
		}
	}
}
