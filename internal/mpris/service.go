package mpris

import (
	"context"
	"errors"
	"sync"

	godbus "github.com/godbus/dbus/v5"
	"github.com/locxl/musicli/internal/log"
)

const (
	mprisBusName    = "org.mpris.MediaPlayer2.musicli"
	lyricsBusName   = "io.github.locxl.musicli"
	mprisPath       = godbus.ObjectPath("/org/mpris/MediaPlayer2")
	lyricsPath      = godbus.ObjectPath("/io/github/locxl/musicli/Lyrics")
	rootIface       = "org.mpris.MediaPlayer2"
	playerIface     = "org.mpris.MediaPlayer2.Player"
	propertiesIface = "org.freedesktop.DBus.Properties"
	lyricsIface     = "io.github.locxl.musicli.Lyrics"
)

type Service struct {
	conn     *godbus.Conn
	log      *log.Logger
	commands chan Command
	mpris    bool
	lyrics   bool

	mu       sync.RWMutex
	snapshot Snapshot
	closed   bool
}

func Start(ctx context.Context, logger *log.Logger, exposeMPRIS bool, exposeLyrics bool) (*Service, error) {
	conn, err := godbus.SessionBus()
	if err != nil {
		return nil, err
	}

	if exposeMPRIS {
		reply, err := conn.RequestName(mprisBusName, godbus.NameFlagDoNotQueue)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if reply != godbus.RequestNameReplyPrimaryOwner {
			conn.Close()
			return nil, errors.New("mpris bus name already owned")
		}
	}
	if exposeLyrics {
		reply, err := conn.RequestName(lyricsBusName, godbus.NameFlagDoNotQueue)
		if err != nil {
			conn.Close()
			return nil, err
		}
		if reply != godbus.RequestNameReplyPrimaryOwner {
			conn.Close()
			return nil, errors.New("lyrics bus name already owned")
		}
	}

	s := &Service{
		conn:     conn,
		log:      logger.WithModule("mpris"),
		commands: make(chan Command, 16),
		mpris:    exposeMPRIS,
		lyrics:   exposeLyrics,
		snapshot: Snapshot{
			PlaybackStatus: StatusStopped,
			LoopStatus:     LoopPlaylist,
			Volume:         80,
			Speed:          1.0,
			CurrentIndex:   -1,
			CurrentLineIdx: -1,
			CurrentWordIdx: -1,
		},
	}

	if exposeMPRIS {
		if err := conn.Export((*rootObject)(s), mprisPath, rootIface); err != nil {
			conn.Close()
			return nil, err
		}
		if err := conn.Export((*playerObject)(s), mprisPath, playerIface); err != nil {
			conn.Close()
			return nil, err
		}
		if err := conn.Export((*mprisPropertiesObject)(s), mprisPath, propertiesIface); err != nil {
			conn.Close()
			return nil, err
		}
	}
	if exposeLyrics {
		if err := conn.Export((*lyricsPropertiesObject)(s), lyricsPath, propertiesIface); err != nil {
			conn.Close()
			return nil, err
		}
		if err := conn.Export((*lyricsObject)(s), lyricsPath, lyricsIface); err != nil {
			conn.Close()
			return nil, err
		}
	}

	go func() {
		<-ctx.Done()
		s.Close()
	}()

	s.log.Info("dbus service started", "mpris", exposeMPRIS, "lyrics", exposeLyrics)
	return s, nil
}

func (s *Service) Commands() <-chan Command {
	return s.commands
}

func (s *Service) Update(snapshot Snapshot) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	prev := s.snapshot
	s.snapshot = snapshot
	s.mu.Unlock()

	if s.mpris && (snapshot.Track != prev.Track ||
		snapshot.PlaybackStatus != prev.PlaybackStatus ||
		snapshot.LoopStatus != prev.LoopStatus ||
		snapshot.Shuffle != prev.Shuffle ||
		snapshot.Volume != prev.Volume ||
		snapshot.Speed != prev.Speed ||
		snapshot.DurationMS != prev.DurationMS ||
		snapshot.LyricText != prev.LyricText) {
		_ = s.emitPlayerProperties(snapshot)
	}
	if s.lyrics && (snapshot.CurrentLine != prev.CurrentLine ||
		snapshot.CurrentLineIdx != prev.CurrentLineIdx ||
		snapshot.CurrentWordIdx != prev.CurrentWordIdx ||
		snapshot.Synced != prev.Synced ||
		snapshot.LyricText != prev.LyricText) {
		_ = s.emitLyricsProperties(snapshot)
	}
}

func (s *Service) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.commands)
	s.mu.Unlock()
	if s.conn != nil {
		if s.mpris {
			_, _ = s.conn.ReleaseName(mprisBusName)
		}
		if s.lyrics {
			_, _ = s.conn.ReleaseName(lyricsBusName)
		}
		s.conn.Close()
	}
}

func (s *Service) snapshotCopy() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot
}

func (s *Service) send(command Command) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return
	}
	select {
	case s.commands <- command:
	default:
		s.log.Warn("dropping mpris command; command queue full", "kind", command.Kind)
	}
}
