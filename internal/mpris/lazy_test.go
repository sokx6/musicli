package mpris

import (
	"context"
	"errors"
	"testing"

	"github.com/locxl/musicli/internal/library"
	"github.com/locxl/musicli/internal/log"
)

type fakeLazyService struct {
	updates []Snapshot
	closed  bool
}

func (s *fakeLazyService) Update(snapshot Snapshot) {
	s.updates = append(s.updates, snapshot)
}

func (s *fakeLazyService) Commands() <-chan Command {
	return nil
}

func (s *fakeLazyService) Close() {
	s.closed = true
}

func TestLazyServiceDoesNotStartWithoutTrack(t *testing.T) {
	starts := 0
	lazy := NewLazyService(context.Background(), log.Discard(), true, true, func(context.Context, *log.Logger, bool, bool) (ManagedService, error) {
		starts++
		return &fakeLazyService{}, nil
	})

	lazy.Update(Snapshot{CurrentIndex: -1, PlaybackStatus: StatusStopped})

	if starts != 0 {
		t.Fatalf("starts = %d, want 0", starts)
	}
}

func TestLazyServiceStartsOnceWhenTrackAppears(t *testing.T) {
	starts := 0
	service := &fakeLazyService{}
	lazy := NewLazyService(context.Background(), log.Discard(), true, true, func(context.Context, *log.Logger, bool, bool) (ManagedService, error) {
		starts++
		return service, nil
	})
	snapshot := Snapshot{Track: &library.Track{Title: "Song"}, CurrentIndex: 0}

	lazy.Update(snapshot)
	lazy.Update(snapshot)

	if starts != 1 {
		t.Fatalf("starts = %d, want 1", starts)
	}
	if len(service.updates) != 2 {
		t.Fatalf("updates = %d, want 2", len(service.updates))
	}
}

func TestLazyServiceRetriesAfterStartFailure(t *testing.T) {
	starts := 0
	service := &fakeLazyService{}
	lazy := NewLazyService(context.Background(), log.Discard(), true, true, func(context.Context, *log.Logger, bool, bool) (ManagedService, error) {
		starts++
		if starts == 1 {
			return nil, errors.New("no session bus")
		}
		return service, nil
	})
	snapshot := Snapshot{Track: &library.Track{Title: "Song"}, CurrentIndex: 0}

	lazy.Update(snapshot)
	lazy.Update(snapshot)

	if starts != 2 {
		t.Fatalf("starts = %d, want 2", starts)
	}
	if len(service.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(service.updates))
	}
}

func TestLazyServiceClosesStartedService(t *testing.T) {
	service := &fakeLazyService{}
	lazy := NewLazyService(context.Background(), log.Discard(), true, true, func(context.Context, *log.Logger, bool, bool) (ManagedService, error) {
		return service, nil
	})

	lazy.Update(Snapshot{Track: &library.Track{Title: "Song"}, CurrentIndex: 0})
	lazy.Close()

	if !service.closed {
		t.Fatal("started service was not closed")
	}
}
