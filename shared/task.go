package shared

import "time"

type TaskType string

const (
	TaskTypeEasy     TaskType = "EASY"
	TaskTypeMedium   TaskType = "MEDIUM"
	TaskTypeHard     TaskType = "HARD"
	TaskTypeVeryHard TaskType = "VERY_HARD"
)

type Task struct {
	ID        string    `json:"id"`
	Type      TaskType  `json:"type"`
	Payload   string    `json:"payload"`
	Priority  int       `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

type TaskResult struct {
	TaskID     string        `json:"task_id"`
	WorkerID   string        `json:"worker_id"`
	Status     string        `json:"status"`
	Duration   time.Duration `json:"duration_ms"`
	FinishedAt time.Time     `json:"finished_at"`
}