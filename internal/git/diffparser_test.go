package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSingleFileOneHunk(t *testing.T) {
	raw := "diff --git a/main.go b/main.go\n" +
		"--- a/main.go\n" +
		"+++ b/main.go\n" +
		"@@ -1,4 +1,5 @@\n" +
		" package main\n" +
		" \n" +
		" func main() {\n" +
		"-\tfmt.Println(\"hello\")\n" +
		"+\tfmt.Println(\"hello world\")\n" +
		"+\tfmt.Println(\"goodbye\")\n" +
		" }"

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)

	f := diff.Files[0]
	assert.Equal(t, "main.go", f.OldPath)
	assert.Equal(t, "main.go", f.NewPath)
	assert.Equal(t, FileModified, f.Status)
	require.Len(t, f.Hunks, 1)

	h := f.Hunks[0]
	assert.Equal(t, 1, h.OldStart)
	assert.Equal(t, 4, h.OldCount)
	assert.Equal(t, 1, h.NewStart)
	assert.Equal(t, 5, h.NewCount)
	require.Len(t, h.Lines, 7)

	assert.Equal(t, DiffLineContext, h.Lines[0].Type)
	assert.Equal(t, "package main", h.Lines[0].Content)
	assert.Equal(t, DiffLineContext, h.Lines[1].Type)
	assert.Equal(t, DiffLineContext, h.Lines[2].Type)
	assert.Equal(t, DiffLineDelete, h.Lines[3].Type)
	assert.Equal(t, "\tfmt.Println(\"hello\")", h.Lines[3].Content)
	assert.Equal(t, DiffLineAdd, h.Lines[4].Type)
	assert.Equal(t, "\tfmt.Println(\"hello world\")", h.Lines[4].Content)
	assert.Equal(t, DiffLineAdd, h.Lines[5].Type)
	assert.Equal(t, "\tfmt.Println(\"goodbye\")", h.Lines[5].Content)
	assert.Equal(t, DiffLineContext, h.Lines[6].Type)
}

func TestParseMultipleFiles(t *testing.T) {
	raw := `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,3 +1,3 @@
 package a
-var x = 1
+var x = 2
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,3 +1,3 @@
 package b
-var y = 1
+var y = 2`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 2)
	assert.Equal(t, "a.go", diff.Files[0].NewPath)
	assert.Equal(t, "b.go", diff.Files[1].NewPath)
}

func TestParseMultipleHunks(t *testing.T) {
	raw := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -1,3 +1,3 @@
 package main
-var a = 1
+var a = 2
@@ -10,3 +10,3 @@
 func foo() {
-	return 1
+	return 2
 }`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	require.Len(t, diff.Files[0].Hunks, 2)

	assert.Equal(t, 1, diff.Files[0].Hunks[0].OldStart)
	assert.Equal(t, 10, diff.Files[0].Hunks[1].OldStart)
}

func TestParseRenamedFile(t *testing.T) {
	raw := `diff --git a/old.go b/new.go
rename from old.go
rename to new.go
--- a/old.go
+++ b/new.go
@@ -1,3 +1,3 @@
 package main
-var x = 1
+var x = 2`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	assert.Equal(t, FileRenamed, diff.Files[0].Status)
	assert.Equal(t, "old.go", diff.Files[0].OldPath)
	assert.Equal(t, "new.go", diff.Files[0].NewPath)
}

func TestParseRenamedFileDifferentPaths(t *testing.T) {
	raw := `diff --git a/old.go b/new.go
--- a/old.go
+++ b/new.go
@@ -1,3 +1,3 @@
 package main
-var x = 1
+var x = 2`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	assert.Equal(t, FileRenamed, diff.Files[0].Status)
}

func TestParseAddedFile(t *testing.T) {
	raw := `diff --git a/new.go b/new.go
--- /dev/null
+++ b/new.go
@@ -0,0 +1,3 @@
+package main
+
+var x = 1`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	assert.Equal(t, FileAdded, diff.Files[0].Status)
	assert.Equal(t, "/dev/null", diff.Files[0].OldPath)
	assert.Equal(t, "new.go", diff.Files[0].NewPath)
}

func TestParseDeletedFile(t *testing.T) {
	raw := `diff --git a/old.go b/old.go
--- a/old.go
+++ /dev/null
@@ -1,3 +0,0 @@
-package main
-
-var x = 1`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	assert.Equal(t, FileDeleted, diff.Files[0].Status)
	assert.Equal(t, "old.go", diff.Files[0].OldPath)
	assert.Equal(t, "/dev/null", diff.Files[0].NewPath)
}

func TestParseEmptyDiff(t *testing.T) {
	diff, err := ParseUnifiedDiff("")
	require.NoError(t, err)
	assert.Empty(t, diff.Files)

	diff, err = ParseUnifiedDiff("   \n  \n")
	require.NoError(t, err)
	assert.Empty(t, diff.Files)
}

func TestParseMalformedInput(t *testing.T) {
	inputs := []string{
		"some random text\nwith multiple lines",
		"diff --git a/x b/x\n@@invalid hunk@@\n",
		"--- a/file\n+++ b/file\n",
	}

	for _, input := range inputs {
		diff, err := ParseUnifiedDiff(input)
		require.NoError(t, err)
		assert.NotNil(t, diff)
	}
}

func TestLineNumbersComputedCorrectly(t *testing.T) {
	raw := `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -5,6 +5,7 @@
 context1
-deleted1
-deleted2
+added1
+added2
+added3
 context2`

	diff, err := ParseUnifiedDiff(raw)
	require.NoError(t, err)
	require.Len(t, diff.Files, 1)
	require.Len(t, diff.Files[0].Hunks, 1)

	lines := diff.Files[0].Hunks[0].Lines

	assert.Equal(t, DiffLineContext, lines[0].Type)
	assert.Equal(t, 5, lines[0].OldLineNo)
	assert.Equal(t, 5, lines[0].NewLineNo)

	assert.Equal(t, DiffLineDelete, lines[1].Type)
	assert.Equal(t, 6, lines[1].OldLineNo)
	assert.Equal(t, 0, lines[1].NewLineNo)

	assert.Equal(t, DiffLineDelete, lines[2].Type)
	assert.Equal(t, 7, lines[2].OldLineNo)
	assert.Equal(t, 0, lines[2].NewLineNo)

	assert.Equal(t, DiffLineAdd, lines[3].Type)
	assert.Equal(t, 0, lines[3].OldLineNo)
	assert.Equal(t, 6, lines[3].NewLineNo)

	assert.Equal(t, DiffLineAdd, lines[4].Type)
	assert.Equal(t, 0, lines[4].OldLineNo)
	assert.Equal(t, 7, lines[4].NewLineNo)

	assert.Equal(t, DiffLineAdd, lines[5].Type)
	assert.Equal(t, 0, lines[5].OldLineNo)
	assert.Equal(t, 8, lines[5].NewLineNo)

	assert.Equal(t, DiffLineContext, lines[6].Type)
	assert.Equal(t, 8, lines[6].OldLineNo)
	assert.Equal(t, 9, lines[6].NewLineNo)
}
