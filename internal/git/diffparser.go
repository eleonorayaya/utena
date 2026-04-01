package git

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var hunkHeaderRegex = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func ParseUnifiedDiff(raw string) (*Diff, error) {
	diff := &Diff{}

	if strings.TrimSpace(raw) == "" {
		return diff, nil
	}

	lines := strings.Split(raw, "\n")
	var currentFile *DiffFile
	var currentHunk *DiffHunk
	var oldLineNo, newLineNo int
	var renameFrom, renameTo string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			if currentFile != nil {
				if currentHunk != nil {
					currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
					currentHunk = nil
				}
				currentFile.Status = determineFileStatus(currentFile, renameFrom, renameTo)
				diff.Files = append(diff.Files, *currentFile)
			}
			currentFile = &DiffFile{}
			renameFrom = ""
			renameTo = ""

		case strings.HasPrefix(line, "rename from "):
			renameFrom = strings.TrimPrefix(line, "rename from ")

		case strings.HasPrefix(line, "rename to "):
			renameTo = strings.TrimPrefix(line, "rename to ")

		case strings.HasPrefix(line, "--- "):
			if currentFile == nil {
				continue
			}
			path := strings.TrimPrefix(line, "--- ")
			if path == "/dev/null" {
				currentFile.OldPath = "/dev/null"
			} else {
				currentFile.OldPath = strings.TrimPrefix(path, "a/")
			}

		case strings.HasPrefix(line, "+++ "):
			if currentFile == nil {
				continue
			}
			path := strings.TrimPrefix(line, "+++ ")
			if path == "/dev/null" {
				currentFile.NewPath = "/dev/null"
			} else {
				currentFile.NewPath = strings.TrimPrefix(path, "b/")
			}

		case strings.HasPrefix(line, "@@"):
			if currentFile == nil {
				continue
			}
			if currentHunk != nil {
				currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
			}
			hunk, old, new_, err := parseHunkHeader(line)
			if err != nil {
				continue
			}
			currentHunk = hunk
			oldLineNo = old
			newLineNo = new_

		case currentHunk != nil && len(line) > 0 && line[0] == ' ':
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:      DiffLineContext,
				Content:   line[1:],
				OldLineNo: oldLineNo,
				NewLineNo: newLineNo,
			})
			oldLineNo++
			newLineNo++

		case currentHunk != nil && len(line) > 0 && line[0] == '+':
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:      DiffLineAdd,
				Content:   line[1:],
				NewLineNo: newLineNo,
			})
			newLineNo++

		case currentHunk != nil && len(line) > 0 && line[0] == '-':
			currentHunk.Lines = append(currentHunk.Lines, DiffLine{
				Type:      DiffLineDelete,
				Content:   line[1:],
				OldLineNo: oldLineNo,
			})
			oldLineNo++
		}
	}

	if currentFile != nil {
		if currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
		}
		currentFile.Status = determineFileStatus(currentFile, renameFrom, renameTo)
		diff.Files = append(diff.Files, *currentFile)
	}

	return diff, nil
}

func parseHunkHeader(line string) (*DiffHunk, int, int, error) {
	matches := hunkHeaderRegex.FindStringSubmatch(line)
	if matches == nil {
		return nil, 0, 0, fmt.Errorf("invalid hunk header: %s", line)
	}

	oldStart, _ := strconv.Atoi(matches[1])
	oldCount := 1
	if matches[2] != "" {
		oldCount, _ = strconv.Atoi(matches[2])
	}
	newStart, _ := strconv.Atoi(matches[3])
	newCount := 1
	if matches[4] != "" {
		newCount, _ = strconv.Atoi(matches[4])
	}

	hunk := &DiffHunk{
		Header:   line,
		OldStart: oldStart,
		OldCount: oldCount,
		NewStart: newStart,
		NewCount: newCount,
	}

	return hunk, oldStart, newStart, nil
}

func determineFileStatus(file *DiffFile, renameFrom, renameTo string) FileStatus {
	if renameFrom != "" && renameTo != "" {
		if file.OldPath == "" {
			file.OldPath = renameFrom
		}
		if file.NewPath == "" {
			file.NewPath = renameTo
		}
		return FileRenamed
	}

	if file.OldPath == "/dev/null" {
		return FileAdded
	}
	if file.NewPath == "/dev/null" {
		return FileDeleted
	}
	if file.OldPath != file.NewPath && file.OldPath != "" && file.NewPath != "" {
		return FileRenamed
	}
	return FileModified
}
