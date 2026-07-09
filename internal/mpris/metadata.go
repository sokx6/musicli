package mpris

import (
	"net/url"

	godbus "github.com/godbus/dbus/v5"
)

func Metadata(s Snapshot) map[string]godbus.Variant {
	trackID := TrackID(s)
	durationMS := s.DurationMS
	if durationMS == 0 && s.Track != nil {
		durationMS = s.Track.Duration
	}

	metadata := map[string]godbus.Variant{
		"mpris:trackid": godbus.MakeVariant(trackID),
		"mpris:length":  godbus.MakeVariant(int64(durationMS) * 1000),
	}
	if s.Track == nil {
		return metadata
	}

	metadata["xesam:title"] = godbus.MakeVariant(s.Track.Title)
	metadata["xesam:artist"] = godbus.MakeVariant([]string{s.Track.Artist})
	metadata["xesam:album"] = godbus.MakeVariant(s.Track.Album)
	if s.Track.AlbumArtist != "" {
		metadata["xesam:albumArtist"] = godbus.MakeVariant([]string{s.Track.AlbumArtist})
	}
	if s.Track.Genre != "" {
		metadata["xesam:genre"] = godbus.MakeVariant([]string{s.Track.Genre})
	}
	if s.Track.Path != "" {
		u := url.URL{Scheme: "file", Path: s.Track.Path}
		metadata["xesam:url"] = godbus.MakeVariant(u.String())
	}
	if s.LyricText != "" {
		metadata["xesam:asText"] = godbus.MakeVariant(s.LyricText)
		metadata["xesam:comment"] = godbus.MakeVariant(s.LyricText)
	}
	return metadata
}

func TrackID(s Snapshot) godbus.ObjectPath {
	idx := s.CurrentIndex
	if idx < 0 {
		idx = 0
	}
	return godbus.ObjectPath("/org/mpris/MediaPlayer2/track/" + itoa(idx))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	negative := v < 0
	if negative {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
