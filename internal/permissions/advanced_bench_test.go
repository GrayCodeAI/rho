package permissions

import "testing"

func BenchmarkClassifier(b *testing.B) {
	c := NewClassifier()
	commands := []string{
		"git status",
		"ls -la",
		"rm -rf /",
		"echo hello",
		"go test ./...",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, cmd := range commands {
			c.Classify(cmd)
		}
	}
}
