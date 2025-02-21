package pause

import (
	"sync"
	"testing"
	"time"
)

func TestNewPauseController(t *testing.T) {
	pc := NewPauseController()
	if pc == nil {
		t.Error("NewPauseController returned nil")
	}
	if pc.stateChan == nil {
		t.Error("stateChan is nil")
	}
}

func TestPauseResume(t *testing.T) {
	pc := NewPauseController()
	
	// Test basic pause
	pc.Pause()
	select {
	case state := <-pc.stateChan:
		if state != Paused {
			t.Errorf("Expected Paused state, got %v", state)
		}
	default:
		t.Error("No state received after Pause")
	}
	
	// Test basic resume
	pc.Resume()
	select {
	case state := <-pc.stateChan:
		if state != Running {
			t.Errorf("Expected Running state, got %v", state)
		}
	default:
		t.Error("No state received after Resume")
	}
}

func TestDrainChannel(t *testing.T) {
	pc := NewPauseController()
	
	// Fill channel
	pc.stateChan <- Running
	
	// Drain and verify it's empty
	pc.drainChannel()
	
	select {
	case <-pc.stateChan:
		t.Error("Channel should be empty after drain")
	default:
		// Channel is empty as expected
	}
}

func TestWaitIfPaused(t *testing.T) {
	pc := NewPauseController()
	termChan := make(chan struct{})
	
	// Test when not paused
	if terminated := pc.WaitIfPaused(termChan); terminated {
		t.Error("WaitIfPaused should return false when not paused")
	}
	
	// Test when paused
	pc.Pause()
	
	var wg sync.WaitGroup
	wg.Add(1)
	
	go func() {
		defer wg.Done()
		if terminated := pc.WaitIfPaused(termChan); terminated {
			t.Error("WaitIfPaused should return false after resume")
		}
	}()
	
	// Give some time for goroutine to start
	time.Sleep(100 * time.Millisecond)
	pc.Resume()
	wg.Wait()
}

func TestWaitIfPausedWithTermination(t *testing.T) {
	pc := NewPauseController()
	termChan := make(chan struct{})
	
	pc.Pause()
	
	var wg sync.WaitGroup
	wg.Add(1)
	
	go func() {
		defer wg.Done()
		if terminated := pc.WaitIfPaused(termChan); !terminated {
			t.Error("WaitIfPaused should return true on termination")
		}
	}()
	
	// give some time for goroutine to start
	time.Sleep(100 * time.Millisecond)
	close(termChan)
	wg.Wait()
}

func TestConcurrentAccess(t *testing.T) {
	pc := NewPauseController()
	var wg sync.WaitGroup
	
	// Test concurrent Pause/Resume calls
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			pc.Pause()
		}()
		go func() {
			defer wg.Done()
			pc.Resume()
		}()
	}
	
	wg.Wait()
}

func TestWaitFunctionality(t *testing.T) {
	pc := NewPauseController()
	termChan := make(chan struct{})

	var wg sync.WaitGroup

	var allowed = false

	pc.Pause()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if terminated := pc.WaitIfPaused(termChan); !terminated {
			if !allowed {
				t.Error("Shouln't wait here")
			}
		} else {
			t.Error("WaitIfPaused should not terminate")
		}
	}()
	time.Sleep(200 * time.Millisecond)

	allowed = true
	pc.Resume()

	wg.Wait()
}

func TestResumeFunctionality(t *testing.T) {
	pc := NewPauseController()
	termChan := make(chan struct{})

	var wg sync.WaitGroup

	var allowed = true

	pc.Resume()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if terminated := pc.WaitIfPaused(termChan); !terminated {
			if !allowed {
				t.Error("Should wait here.")
			}
		} else {
			t.Error("WaitIfPaused should not terminate")
		}
	}()
	time.Sleep(200 * time.Millisecond)

	allowed = false
	pc.Resume()

	wg.Wait()
}
