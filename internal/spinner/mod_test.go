package spinner_test

import (
	"strings"
	"testing"
	"time"

	"github.com/magicdrive/ark/internal/spinner"
)

var testFrames = []string{"⠋", "⠙", "⠹"}

func TestSpinnerStartStop(t *testing.T) {
	s := spinner.New(50*time.Millisecond, "Testing...")

	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop("Done!")
}

func TestSpinnerSetMessage(t *testing.T) {
	s := spinner.New(30*time.Millisecond, "Init")
	s.Start()
	time.Sleep(60 * time.Millisecond)
	s.SetMessage("Step2")
	time.Sleep(60 * time.Millisecond)
	s.Stop("Finish!")
}

func TestSpinnerMultipleStartStop(t *testing.T) {
	s := spinner.New(10*time.Millisecond, "Multi")
	for range 3 {
		s.Start()
		time.Sleep(30 * time.Millisecond)
		s.Stop()
	}
}

func TestSpinnerStopWithoutStart(t *testing.T) {
	s := spinner.New(10*time.Millisecond, "NoStart")
	s.Stop()
}

func TestSpinnerWithLongMessage(t *testing.T) {
	s := spinner.New(20*time.Millisecond, strings.Repeat("X", 50))
	s.Start()
	time.Sleep(60 * time.Millisecond)
	s.Stop("Done!")
}
