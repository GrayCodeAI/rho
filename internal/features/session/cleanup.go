package session

// CleanupHooks describes cancellable work owned by a live interactive
// session. The feature package owns ordering; the CLI supplies the concrete
// callbacks because their implementations belong to Bubble Tea and the
// runtime.
type CleanupHooks struct {
	LoopCancel     func()
	ParallelCancel func()
	SleepCancel    func()
	WatcherStop    func()
}

// StopBackgroundWork cancels session-scoped work in a stable order. Nil hooks
// are intentionally accepted so callers can select the cleanup scope without
// duplicating cancellation conditionals.
func StopBackgroundWork(h CleanupHooks) {
	if h.LoopCancel != nil {
		h.LoopCancel()
	}
	if h.ParallelCancel != nil {
		h.ParallelCancel()
	}
	if h.SleepCancel != nil {
		h.SleepCancel()
	}
	if h.WatcherStop != nil {
		h.WatcherStop()
	}
}
