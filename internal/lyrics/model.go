package lyrics

// Line is a normalized lyric line.
type Line struct {
	StartMs      int
	EndMs        int
	Text         string
	Words        []Word
	Translation  string
	Agent        string
	IsBackground bool
}

// Word is a lyric segment with word-level timing.
type Word struct {
	Text    string
	StartMs int
	EndMs   int
}

// Lyric is the normalized lyric document.
type Lyric struct {
	Lines []Line
	Tags  map[string]string
}

// Parser parses one lyric format.
type Parser interface {
	Parse(text string) (*Lyric, error)
	Format() string
}
