package mpris

import (
	"context"
	"sync"

	"github.com/locxl/musicli/internal/log"
)

type ManagedService interface {
	Update(Snapshot)
	Commands() <-chan Command
	Close()
}

type StartFunc func(context.Context, *log.Logger, bool, bool) (ManagedService, error)

type LazyService struct {
	ctx          context.Context
	log          *log.Logger
	exposeMPRIS  bool
	exposeLyrics bool
	start        StartFunc

	mu       sync.Mutex
	service  ManagedService
	commands chan Command
	closed   bool
}

func NewLazyService(ctx context.Context, logger *log.Logger, exposeMPRIS bool, exposeLyrics bool, start StartFunc) *LazyService {
	return &LazyService{
		ctx:          ctx,
		log:          logger,
		exposeMPRIS:  exposeMPRIS,
		exposeLyrics: exposeLyrics,
		start:        start,
		commands:     make(chan Command, 16),
	}
}

func (s *LazyService) Commands() <-chan Command {
	return s.commands
}

func (s *LazyService) Update(snapshot Snapshot) {
	if snapshot.Track == nil {
		return
	}

	service := s.ensureStarted()
	if service == nil {
		return
	}
	service.Update(snapshot)
}

func (s *LazyService) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	service := s.service
	s.mu.Unlock()

	if service != nil {
		service.Close()
	}
	close(s.commands)
}

func (s *LazyService) ensureStarted() ManagedService {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	if s.service != nil {
		service := s.service
		s.mu.Unlock()
		return service
	}
	start := s.start
	if start == nil {
		start = startManagedService
	}
	s.mu.Unlock()

	service, err := start(s.ctx, s.log, s.exposeMPRIS, s.exposeLyrics)
	if err != nil {
		s.log.WithModule("mpris").Warn("mpris service unavailable", "err", err)
		return nil
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		service.Close()
		return nil
	}
	if s.service != nil {
		existing := s.service
		s.mu.Unlock()
		service.Close()
		return existing
	}
	s.service = service
	s.mu.Unlock()

	go s.forwardCommands(service)
	return service
}

func (s *LazyService) forwardCommands(service ManagedService) {
	for command := range service.Commands() {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return
		}
		select {
		case s.commands <- command:
		default:
			s.log.WithModule("mpris").Warn("dropping mpris command; lazy command queue full", "kind", command.Kind)
		}
		s.mu.Unlock()
	}
}

func startManagedService(ctx context.Context, logger *log.Logger, exposeMPRIS bool, exposeLyrics bool) (ManagedService, error) {
	return Start(ctx, logger, exposeMPRIS, exposeLyrics)
}
