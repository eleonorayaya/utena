package git

type DiffLineType string

const (
	DiffLineContext DiffLineType = "context"
	DiffLineAdd     DiffLineType = "add"
	DiffLineDelete  DiffLineType = "delete"
)

type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileDeleted  FileStatus = "deleted"
	FileRenamed  FileStatus = "renamed"
)

type DiffLine struct {
	Type      DiffLineType `json:"type"`
	Content   string       `json:"content"`
	OldLineNo int          `json:"old_line_no,omitempty"`
	NewLineNo int          `json:"new_line_no,omitempty"`
}

type DiffHunk struct {
	Header   string     `json:"header"`
	OldStart int        `json:"old_start"`
	OldCount int        `json:"old_count"`
	NewStart int        `json:"new_start"`
	NewCount int        `json:"new_count"`
	Lines    []DiffLine `json:"lines"`
}

type DiffFile struct {
	OldPath string     `json:"old_path"`
	NewPath string     `json:"new_path"`
	Status  FileStatus `json:"status"`
	Hunks   []DiffHunk `json:"hunks"`
}

type Diff struct {
	Files []DiffFile `json:"files"`
}
