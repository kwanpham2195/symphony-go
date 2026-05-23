package runner

import (
	"sync"
	"testing"
)

func TestUpdatePrompt_ConcurrentAccess(t *testing.T) {
	r := &Runner{}
	r.UpdatePrompt("initial")

	var wg sync.WaitGroup
	wg.Add(2)

	// Writer: update prompt concurrently
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.UpdatePrompt("updated prompt")
		}
	}()

	// Reader: read prompt concurrently (simulates Run reading the prompt)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p := r.getPrompt()
			if p != "initial" && p != "updated prompt" {
				t.Errorf("unexpected prompt: %q", p)
			}
		}
	}()

	wg.Wait()
}
