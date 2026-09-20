package session

import (
	"reflect"
	"testing"
)

func TestStopBackgroundWorkUsesStableOrder(t *testing.T) {
	var got []string
	StopBackgroundWork(CleanupHooks{
		LoopCancel:     func() { got = append(got, "loop") },
		ParallelCancel: func() { got = append(got, "parallel") },
		SleepCancel:    func() { got = append(got, "sleep") },
		WatcherStop:    func() { got = append(got, "watcher") },
	})
	if want := []string{"loop", "parallel", "sleep", "watcher"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup order = %v, want %v", got, want)
	}
}
