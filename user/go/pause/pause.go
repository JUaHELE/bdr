package pause

import "sync"

type PauseState int

const (
	Running PauseState = iota
	Paused
)

// implementation intended for one waiter

// implements pause mechanism for threads or goroutines
type PauseController struct {
	stateChan chan PauseState
	mu        sync.RWMutex
}

// initilazer...
func NewPauseController() *PauseController {
	return &PauseController{
		stateChan: make(chan PauseState, 1),
	}
}

// drains channel if there is some value
func (p *PauseController) drainChannel() {
	select {
	case <-p.stateChan:
	default:
	}
}

// pauses the PauseController
func (p *PauseController) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.drainChannel()
	p.stateChan <- Paused
}

// resumes the pauseController
func (p *PauseController) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.drainChannel()
	p.stateChan <- Running
}

func (p *PauseController) IsPaused() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	// Make a non-blocking check to see if there's a state in the channel
	select {
	case state := <-p.stateChan:
		// Put the state back in the channel for other operations
		p.stateChan <- state
		return state == Paused
	default:
		// If channel is empty, default to Running state
		return false
	}
}

// false is returned if its not by termination, true otherwise
func (p *PauseController) waitUntilResumed(termChan <-chan struct{}) bool {
	for {
		select {
		case <-termChan:
			return true
		case state := <-p.stateChan:
			if state == Running {
				return false
			}
		}
	}
}

// false is returned if its not by termination, true otherwise
func (p *PauseController) WaitIfPaused(termChan <-chan struct{}) bool {
	select {
	case <-termChan:
		return true
	case state := <-p.stateChan:
		if state == Paused {
			return p.waitUntilResumed(termChan)
		}
		return false
	default:
		return false
	}
}
