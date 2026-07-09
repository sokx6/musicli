package mpris

import "github.com/locxl/musicli/internal/library"

type PlaybackStatus string

const (
	StatusPlaying PlaybackStatus = "Playing"
	StatusPaused  PlaybackStatus = "Paused"
	StatusStopped PlaybackStatus = "Stopped"
)

type LoopStatus string

const (
	LoopNone     LoopStatus = "None"
	LoopTrack    LoopStatus = "Track"
	LoopPlaylist LoopStatus = "Playlist"
)

type Snapshot struct {
	Track          *library.Track
	CurrentIndex   int
	PlaybackStatus PlaybackStatus
	LoopStatus     LoopStatus
	Shuffle        bool
	PositionMS     int
	DurationMS     int
	Volume         int
	Speed          float64
	LyricText      string
	LyricFormat    string
	CurrentLine    string
	CurrentLineIdx int
	CurrentWordIdx int
	Synced         bool
}

type CommandKind int

const (
	CmdNext CommandKind = iota
	CmdPrevious
	CmdPlay
	CmdPause
	CmdPlayPause
	CmdStop
	CmdSeek
	CmdSetPosition
)

type Command struct {
	Kind       CommandKind
	OffsetUS   int64
	PositionUS int64
	TrackID    string
}
