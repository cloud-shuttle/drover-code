package diff

type Hunk struct {
	Header     string
	OldContent []string
	NewContent []string
	Context    []string
	OldStart   int
	OldLines   int
	RawLines   []string
	Accepted   bool
	Rejected   bool
}

type DiffModel struct {
	FilePath string
	Hunks    []Hunk
	Cursor   int
}
