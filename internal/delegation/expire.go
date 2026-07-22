package delegation

import "time"

// MaybeExpireTask checks if the task is past its deadline and non-terminal.
// If so, it returns the task with status set to EXPIRED and a CAS-ready copy.
// The caller must CAS the task to persist the expiration.
//
// Returns (expiredTask, shouldExpire). If shouldExpire is false, the task
// is either already terminal or not past deadline.
func MaybeExpireTask(task *Task, now time.Time) (*Task, bool) {
	if task == nil {
		return nil, false
	}
	if task.Status.IsTerminal() {
		return nil, false
	}
	if task.DeadlineAt == nil {
		return nil, false
	}
	if now.Before(*task.DeadlineAt) {
		return nil, false
	}
	// Past deadline, non-terminal — expire it.
	expired := *task
	expired.Status = TaskStatusExpired
	expired.UpdatedAt = now
	return &expired, true
}