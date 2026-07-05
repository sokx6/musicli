package library

// Track represents a single audio file in the library.
type Track struct {
	Path        string
	Title       string
	Artist      string
	AlbumArtist string
	Album       string
	Composer    string
	Genre       string
	Year        int
	TrackNo     int
	DiscNo      int
	Duration    int   // milliseconds (from ffprobe; 0 if unknown)
	Size        int64 // bytes
	HasCover    bool
}

// Album groups tracks by album name.
type Album struct {
	Name        string
	AlbumArtist string
	Tracks      []*Track
}
