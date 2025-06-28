package spinner

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Spinner struct {
	frames   []string
	interval time.Duration
	msg      string

	stopCh chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
}

func New(interval time.Duration, msg string) *Spinner {
	return &Spinner{
		frames:   spinnerFrames,
		interval: interval,
		msg:      msg,
		stopCh:   make(chan struct{}),
	}
}

func (s *Spinner) SetMessage(msg string) {
	s.mu.Lock()
	s.msg = msg
	s.mu.Unlock()
}

func (s *Spinner) Start() {
	s.stopCh = make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		i := 0
		for {
			select {
			case <-s.stopCh:
				fmt.Print("\r\x1b[K")
				return
			default:
				s.mu.Lock()
				msg := s.msg
				s.mu.Unlock()
				frame := s.frames[i%len(s.frames)]
				fmt.Printf("\r%s %s", frame, msg)
				time.Sleep(s.interval)
				i++
			}
		}
	}()
}

func (s *Spinner) Stop(finalMsg ...string) {
	close(s.stopCh)
	s.wg.Wait()
	if len(finalMsg) > 0 {
		fmt.Printf("\r\x1b[K%s\n", finalMsg[0])
	} else {
		fmt.Print("\r\x1b[K")
	}
}
