package mpris

import godbus "github.com/godbus/dbus/v5"

type rootObject Service
type playerObject Service
type mprisPropertiesObject Service
type lyricsPropertiesObject Service
type lyricsObject Service

func (o *rootObject) Raise() *godbus.Error { return nil }
func (o *rootObject) Quit() *godbus.Error  { return nil }

func (o *playerObject) Next() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdNext})
	return nil
}

func (o *playerObject) Previous() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdPrevious})
	return nil
}

func (o *playerObject) Pause() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdPause})
	return nil
}

func (o *playerObject) PlayPause() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdPlayPause})
	return nil
}

func (o *playerObject) Stop() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdStop})
	return nil
}

func (o *playerObject) Play() *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdPlay})
	return nil
}

func (o *playerObject) Seek(offset int64) *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdSeek, OffsetUS: offset})
	return nil
}

func (o *playerObject) SetPosition(trackID godbus.ObjectPath, position int64) *godbus.Error {
	(*Service)(o).send(Command{Kind: CmdSetPosition, TrackID: string(trackID), PositionUS: position})
	return nil
}

func (o *playerObject) OpenUri(uri string) *godbus.Error {
	return godbus.NewError("org.mpris.MediaPlayer2.Player.Error.NotSupported", []any{"OpenUri is not supported"})
}

func (o *mprisPropertiesObject) Get(iface string, property string) (godbus.Variant, *godbus.Error) {
	s := (*Service)(o).snapshotCopy()
	props, ok := mprisPropertiesForInterface(iface, s)
	if !ok {
		return godbus.MakeVariant(nil), dbusError("org.freedesktop.DBus.Error.UnknownInterface", iface)
	}
	value, ok := props[property]
	if !ok {
		return godbus.MakeVariant(nil), dbusError("org.freedesktop.DBus.Error.InvalidArgs", property)
	}
	return value, nil
}

func (o *mprisPropertiesObject) GetAll(iface string) (map[string]godbus.Variant, *godbus.Error) {
	s := (*Service)(o).snapshotCopy()
	props, ok := mprisPropertiesForInterface(iface, s)
	if !ok {
		return nil, dbusError("org.freedesktop.DBus.Error.UnknownInterface", iface)
	}
	return props, nil
}

func (o *mprisPropertiesObject) Set(iface string, property string, value godbus.Variant) *godbus.Error {
	return dbusError("org.freedesktop.DBus.Error.PropertyReadOnly", iface+"."+property)
}

func (o *lyricsPropertiesObject) Get(iface string, property string) (godbus.Variant, *godbus.Error) {
	s := (*Service)(o).snapshotCopy()
	props, ok := lyricsPropertiesForInterface(iface, s)
	if !ok {
		return godbus.MakeVariant(nil), dbusError("org.freedesktop.DBus.Error.UnknownInterface", iface)
	}
	value, ok := props[property]
	if !ok {
		return godbus.MakeVariant(nil), dbusError("org.freedesktop.DBus.Error.InvalidArgs", property)
	}
	return value, nil
}

func (o *lyricsPropertiesObject) GetAll(iface string) (map[string]godbus.Variant, *godbus.Error) {
	s := (*Service)(o).snapshotCopy()
	props, ok := lyricsPropertiesForInterface(iface, s)
	if !ok {
		return nil, dbusError("org.freedesktop.DBus.Error.UnknownInterface", iface)
	}
	return props, nil
}

func (o *lyricsPropertiesObject) Set(iface string, property string, value godbus.Variant) *godbus.Error {
	return dbusError("org.freedesktop.DBus.Error.PropertyReadOnly", iface+"."+property)
}

func mprisPropertiesForInterface(iface string, s Snapshot) (map[string]godbus.Variant, bool) {
	switch iface {
	case rootIface:
		return rootProperties(), true
	case playerIface:
		return playerProperties(s), true
	default:
		return nil, false
	}
}

func lyricsPropertiesForInterface(iface string, s Snapshot) (map[string]godbus.Variant, bool) {
	switch iface {
	case lyricsIface:
		return lyricsProperties(s), true
	default:
		return nil, false
	}
}

func rootProperties() map[string]godbus.Variant {
	return map[string]godbus.Variant{
		"CanQuit":             godbus.MakeVariant(false),
		"CanRaise":            godbus.MakeVariant(false),
		"HasTrackList":        godbus.MakeVariant(false),
		"Identity":            godbus.MakeVariant("musicli"),
		"DesktopEntry":        godbus.MakeVariant("musicli"),
		"SupportedUriSchemes": godbus.MakeVariant([]string{}),
		"SupportedMimeTypes":  godbus.MakeVariant([]string{}),
	}
}

func playerProperties(s Snapshot) map[string]godbus.Variant {
	rate := s.Speed
	if rate == 0 {
		rate = 1.0
	}
	return map[string]godbus.Variant{
		"PlaybackStatus": godbus.MakeVariant(string(s.PlaybackStatus)),
		"LoopStatus":     godbus.MakeVariant(string(s.LoopStatus)),
		"Rate":           godbus.MakeVariant(rate),
		"Shuffle":        godbus.MakeVariant(s.Shuffle),
		"Metadata":       godbus.MakeVariant(Metadata(s)),
		"Volume":         godbus.MakeVariant(float64(s.Volume) / 100.0),
		"Position":       godbus.MakeVariant(int64(s.PositionMS) * 1000),
		"MinimumRate":    godbus.MakeVariant(0.5),
		"MaximumRate":    godbus.MakeVariant(2.0),
		"CanGoNext":      godbus.MakeVariant(true),
		"CanGoPrevious":  godbus.MakeVariant(true),
		"CanPlay":        godbus.MakeVariant(true),
		"CanPause":       godbus.MakeVariant(true),
		"CanSeek":        godbus.MakeVariant(true),
		"CanControl":     godbus.MakeVariant(true),
	}
}

func lyricsProperties(s Snapshot) map[string]godbus.Variant {
	return map[string]godbus.Variant{
		"CurrentLine":      godbus.MakeVariant(s.CurrentLine),
		"CurrentLineIndex": godbus.MakeVariant(int32(s.CurrentLineIdx)),
		"CurrentWordIndex": godbus.MakeVariant(int32(s.CurrentWordIdx)),
		"Synced":           godbus.MakeVariant(s.Synced),
		"LyricText":        godbus.MakeVariant(s.LyricText),
		"LyricFormat":      godbus.MakeVariant(s.LyricFormat),
	}
}

func (s *Service) emitPlayerProperties(snapshot Snapshot) error {
	changed := map[string]godbus.Variant{
		"PlaybackStatus": godbus.MakeVariant(string(snapshot.PlaybackStatus)),
		"LoopStatus":     godbus.MakeVariant(string(snapshot.LoopStatus)),
		"Shuffle":        godbus.MakeVariant(snapshot.Shuffle),
		"Metadata":       godbus.MakeVariant(Metadata(snapshot)),
		"Volume":         godbus.MakeVariant(float64(snapshot.Volume) / 100.0),
		"Rate":           godbus.MakeVariant(snapshot.Speed),
	}
	return s.conn.Emit(mprisPath, propertiesIface+".PropertiesChanged", playerIface, changed, []string{"Position"})
}

func (s *Service) emitLyricsProperties(snapshot Snapshot) error {
	changed := lyricsProperties(snapshot)
	if err := s.conn.Emit(lyricsPath, propertiesIface+".PropertiesChanged", lyricsIface, changed, []string{}); err != nil {
		return err
	}
	return s.conn.Emit(lyricsPath, lyricsIface+".CurrentLineChanged", snapshot.CurrentLine, int32(snapshot.CurrentLineIdx), int32(snapshot.CurrentWordIdx))
}

func dbusError(name string, detail string) *godbus.Error {
	return godbus.NewError(name, []any{detail})
}
