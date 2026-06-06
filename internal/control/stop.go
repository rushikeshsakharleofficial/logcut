package control

import (
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
)

type Stopper struct {
	requested atomic.Bool
	reason    atomic.Value
	ch        chan os.Signal
}

func NewStopper() *Stopper {
	s := &Stopper{ch: make(chan os.Signal, 2)}
	s.reason.Store("")
	signal.Notify(s.ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-s.ch
		s.requested.Store(true)
		s.reason.Store(sig.String())
	}()
	return s
}

func (s *Stopper) Stop() {
	signal.Stop(s.ch)
}

func (s *Stopper) Requested() bool {
	return s.requested.Load()
}

func (s *Stopper) Reason() string {
	v, _ := s.reason.Load().(string)
	if v == "" {
		return "signal"
	}
	return v
}
