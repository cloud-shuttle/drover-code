# 04 — File System Tools

**Package:** `internal/tools/fs`  
**Files:** `read.go`, `write.go`, `edit.go`, `ls.go`  
**Tools:** `read_file`, `write_file`, `edit_file`, `list_directory`, `file_info`  
**Depends on:** `internal/tools/toolutil`

---

## Purpose

The file system tools are the most frequently called tools in any coding
session. A typical "refactor this function" task might involve:

1. `read_file` (understand the current code)
2. `glob` (find related files)
3. `read_file` × 3 (read the related files, in parallel)
4. `edit_file` × 2 (make the changes)
5. `read_file` (verify the changes look right)

Getting these tools right — in terms of correctness, safety, and the quality
of information returned to the model — has a larger impact on overall system
quality than almost any other implementation decision.

---

## 1. `read_file`

### 1.1 Core behaviour

```go
type ReadFile struct {
    WorkDir string
}

type readFileInput struct {
    Path      string `json:"path"`
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
}
```

Read a file, optionally sliced to a line range, and return it as a string
suitable for the model to reason about.

### 1.2 Binary detection

```go
func isBinary(data []byte) bool {
    sample := data
    if len(sample) > 8192 {
        sample = sample[:8192]
    }
    if !utf8.Valid(sample) {
        return true
    }
    for _, b := range sample {
        if b == 0 {
            return true
        }
    }
    return false
}
```

Binary detection runs on the first 8 KB. Two conditions trigger it:

**Invalid UTF-8.** Source code files are overwhelmingly UTF-8. If the first
8 KB is not valid UTF-8 the file is almost certainly binary. The 8 KB sample
is large enough to catch valid UTF-8 files that contain occasional non-ASCII
characters (comments in non-English languages, string literals with accented
characters).

**Null byte.** Null bytes never appear in text files but are common in binary
formats (compiled binaries, images, database files). Even a single null byte
in the first 8 KB is a strong signal the file is binary.

Why refuse binary files? Sending binary content to the model wastes context
and produces garbage — the model will see a sequence of bytes that don't
correspond to any meaningful representation. The error message points the
model toward alternative approaches (e.g. `file_info` to check the file type,
`bash` with appropriate tools to inspect it).

### 1.3 Line range selection

```go
func sliceLines(content string, start, end int) (string, error) {
    lines := strings.Split(content, "\n")
    total := len(lines)

    if start == 0 { start = 1 }
    if end == 0   { end = total }
    if start < 1  { start = 1 }
    if end > total { end = total }
    if start > end {
        return "", fmt.Errorf("start_line (%d) > end_line (%d)", start, end)
    }

    var b strings.Builder
    for i := start; i <= end; i++ {
        fmt.Fprintf(&b, "%6d\t%s\n", i, lines[i-1])
    }
    return b.String(), nil
}
```

Line numbers are 1-based (matching editor conventions) and inclusive on both
ends. Zero values mean "use the natural boundary" — `start_line: 0` means
start from line 1, `end_line: 0` means read to the last line.

**Why annotate with line numbers?** When the model reads a file and then
calls `edit_file`, it needs to be able to describe what it's changing in
terms the user understands. Line numbers in `read_file` output let the model
say "I see the issue on line 47" in its response, and they help construct
precise `old_string` values for `edit_file` that include enough surrounding
context to be unique.

The `%6d\t` format right-aligns line numbers in a 6-character field, followed
by a tab. This mirrors the output of tools like `cat -n` and `less -N`. The
tab separator ensures code indentation is preserved without ambiguity about
whether leading spaces are part of the line number or the content.

**The split-then-annotate approach** has a subtle issue with files that don't
end in a newline. `strings.Split("abc\ndef", "\n")` produces `["abc", "def"]`
— correct. `strings.Split("abc\ndef\n", "\n")` produces `["abc", "def", ""]`
— an empty final element. This means a file ending with a newline appears to
have one extra empty line. The model should ignore blank trailing lines, but
it's worth being aware of. A future fix: strip the trailing empty element
if it's empty and the content ends with `\n`.

### 1.4 Output truncation

After line slicing (if any), the result passes through `toolutil.Truncate`.
This means a caller can request lines 1–50000 and still get at most 200,000
bytes back. The truncation note in the output tells the model where the cap
was applied.

For very large files, the model should use line ranges to read sections rather
than the whole file. The description mentions this:

```
"Use start_line and end_line to read a specific range for large files."
```

### 1.5 What `read_file` does not do

- It does not follow symlinks. `os.ReadFile` reads the symlink target, so
  symlinks are transparently followed. This is the correct behaviour for a
  coding assistant.
- It does not stat the file before reading. The `file_info` tool handles
  metadata queries.
- It does not decode non-UTF-8 encodings. Files in Latin-1, UTF-16, or
  Windows-1252 are rejected as binary. This is technically incorrect for
  some legitimate source files but is the right trade-off — handling all
  possible encodings is complex, and the vast majority of source code is UTF-8.

---

## 2. `write_file`

### 2.1 Core behaviour

Creates or completely overwrites a file. This is the nuclear option —
`edit_file` is always preferred for modifying existing files. `write_file`
is appropriate for:

- Creating new files that don't exist yet
- Generating entirely new content (e.g. a new test file, a config template)
- Cases where the model needs to replace a file wholesale

The description steers the model toward `edit_file`:
```
"For making targeted changes to an existing file, prefer edit_file instead."
```

### 2.2 Permission handling

```go
func (t *WriteFile) NeedsPermission(_ json.RawMessage) bool { return true }
```

Every `write_file` call requires permission. Unlike `read_file`, there is no
input that makes `write_file` safe to auto-approve — it always overwrites the
target, destroying the previous contents.

### 2.3 Implementation

```go
func (t *WriteFile) Execute(_ context.Context, rawInput json.RawMessage) (string, error) {
    var inp writeFileInput
    json.Unmarshal(rawInput, &inp)

    absPath, err := toolutil.SafePath(t.WorkDir, inp.Path)
    // ...

    // Create parent directories
    os.MkdirAll(filepath.Dir(absPath), 0o755)

    // Preserve existing permissions
    perm := os.FileMode(0o644)
    if info, err := os.Stat(absPath); err == nil {
        perm = info.Mode().Perm()
    }

    toolutil.WriteAtomic(absPath, []byte(inp.Content), perm)
    return fmt.Sprintf("wrote %d bytes to %s", len(inp.Content), inp.Path), nil
}
```

**Preserving existing permissions** is important for executable files. If the
model overwrites a shell script or a Python file that had `chmod +x`, the new
version should remain executable. Without this check, `write_file` would
silently strip the executable bit, breaking scripts.

**Creating parent directories** with `os.MkdirAll` means the model can create
a new file in a new directory without first calling `bash` to `mkdir -p`. This
is the expected behaviour — if you ask for `src/handlers/auth.go` to be created,
the `handlers` directory should be created automatically.

**Output format:** `"wrote 42 bytes to src/main.go"` — minimal but informative.
The model knows the write succeeded and knows the path, which it can reference
in its explanation to the user.

---

## 3. `edit_file`

This is the most complex and most important file system tool. A large fraction
of the model's usefulness depends on `edit_file` working correctly.

### 3.1 The problem with exact matching

The obvious implementation of `edit_file` is a simple string replacement:

```go
result := strings.Replace(content, inp.OldString, inp.NewString, 1)
```

This works when the model reproduces the `old_string` perfectly. But models
frequently make minor whitespace errors:

- Trailing spaces on lines (invisible in most editors)
- Mixed tabs and spaces
- Different numbers of blank lines between sections
- Indentation that's off by one space

These errors cause exact matching to fail even when the model's intent is
clearly correct. A strict implementation requires the model to retry, which
wastes tokens and user time, and makes the tool feel brittle.

### 3.2 Three-pass matching

```
Pass 1: Exact match
Pass 2: Whitespace-normalised match
Pass 3: Not found — return descriptive error
```

Each pass also checks for ambiguity: if the match count is > 1, we refuse
and ask the model to be more specific. An ambiguous replacement is worse than
a failed one — it would silently change the wrong location.

### 3.3 Pass 1: Exact match

```go
count := strings.Count(original, inp.OldString)
if count == 1 {
    return strings.Replace(original, inp.OldString, inp.NewString, 1)
}
if count > 1 {
    return fmt.Errorf("old_string matches %d locations — make it more specific", count)
}
// count == 0: fall through to fuzzy match
```

Exact match is fast (O(n) in file length) and produces a diff that exactly
reflects what the model intended. Most calls succeed at this pass.

### 3.4 Pass 2: Whitespace normalisation

```go
func normaliseWS(s string) string {
    lines := strings.Split(s, "\n")
    for i, l := range lines {
        fields := strings.FieldsFunc(l, unicode.IsSpace)
        lines[i] = strings.Join(fields, " ")
    }
    return strings.Join(lines, "\n")
}
```

Normalisation is applied to each line independently:

1. Split the line into fields on any Unicode whitespace character
2. Rejoin fields with a single space

This normalises:
- `    func foo()  {` → `func foo() {`
- `\t\treturn x   ` → `return x`
- `   x := 1   ` → `x := 1`

Importantly, normalisation happens **line by line**, not globally. This
preserves the structure of the `old_string` — blank lines between code blocks
remain as blank lines (they normalise to empty strings, not spaces).

The fuzzy count check runs on the normalised file content:

```go
normFile := normaliseWS(original)
normOld  := normaliseWS(inp.OldString)
fuzzyCount := strings.Count(normFile, normOld)
// same ambiguity check: refuse if > 1
```

### 3.5 Fuzzy replacement: locating the original text

Finding the match in the normalised file is easy. The hard part is applying
the replacement to the **original** (un-normalised) file. We need to know
which lines in the original correspond to the fuzzy match.

```go
func fuzzyReplace(original, oldStr, newStr string) (string, bool) {
    oldLines  := strings.Split(oldStr, "\n")
    fileLines := strings.Split(original, "\n")

    normOldLines  := normaliseLines(oldLines)
    normFileLines := normaliseLines(fileLines)

    for startIdx := 0; startIdx <= len(fileLines)-len(oldLines); startIdx++ {
        // Check if all old lines match consecutively at this position
        match := true
        for j, normOld := range normOldLines {
            if normFileLines[startIdx+j] != normOld {
                match = false
                break
            }
        }
        if !match { continue }

        // Found the region — splice in newStr
        endIdx := startIdx + len(oldLines)
        prefix := strings.Join(fileLines[:startIdx], "\n")
        suffix := strings.Join(fileLines[endIdx:], "\n")

        // Handle edge cases: empty prefix/suffix
        switch {
        case prefix == "" && suffix == "":
            return newStr, true
        case prefix == "":
            return newStr + "\n" + suffix, true
        case suffix == "":
            return prefix + "\n" + newStr, true
        default:
            return prefix + "\n" + newStr + "\n" + suffix, true
        }
    }
    return "", false
}
```

The algorithm:
1. Split both strings into lines
2. Normalise each line individually
3. Slide a window of `len(oldLines)` across the file
4. At each position, check all lines match in order
5. On match, splice: `prefix + newStr + suffix`

The splice handles four edge cases based on whether the prefix and suffix are
empty. Without these guards, replacing lines at the start or end of a file
would produce leading or trailing newlines that weren't in the original.

**Why not use the normalised line's position in the normalised file?**
That approach works when the normalised file has the same number of lines as
the original (which is always true since we normalise line-by-line). But it's
more complex to implement correctly than the sliding window approach, which
operates entirely on the original line structure.

### 3.6 Unified diff output

After a successful replacement, we return a unified diff:

```go
func unifiedDiff(path, original, updated string) string {
    if original == updated { return "no changes" }

    origLines := strings.Split(original, "\n")
    newLines  := strings.Split(updated, "\n")

    // Find first and last differing lines
    firstDiff := 0
    for firstDiff < len(origLines) && firstDiff < len(newLines) {
        if origLines[firstDiff] != newLines[firstDiff] { break }
        firstDiff++
    }
    lastOrig, lastNew := len(origLines)-1, len(newLines)-1
    for lastOrig > firstDiff && lastNew > firstDiff {
        if origLines[lastOrig] != newLines[lastNew] { break }
        lastOrig--
        lastNew--
    }

    // Emit context + changed lines
    contextBefore := max(0, firstDiff-3)
    // ...
}
```

The diff is intentionally minimal — a simple greedy algorithm that finds the
first and last differing lines and shows 3 lines of context on each side. This
is not a true LCS-based diff (which would handle interleaved insertions and
deletions more accurately), but it's correct and sufficient for the typical
case of replacing a contiguous block of code.

**Why return a diff at all?** The diff serves as confirmation to the model.
After calling `edit_file`, the model reads the diff and can verify:
- The right section was changed
- The replacement looks syntactically correct
- No unintended content was removed or added

Without a diff, the model's only option to verify the change is to call
`read_file` again — an extra round-trip. The diff short-circuits this.

**Format:** We use unified diff (`--- file` / `+++ file` / `@@ -N,M +N,M @@`)
because it's the format every developer knows, it's what `git diff` produces,
and the model has been trained on vast amounts of it.

### 3.7 What makes a good `old_string`

The model's ability to construct a good `old_string` is critical. The tool
description guides it:

```
"old_string must match exactly one location in the file.
Include surrounding lines for context if needed."
```

In practice, the model should:

**Include enough context to be unique.** A short `old_string` like `return nil`
almost certainly appears multiple times. Adding the surrounding function
signature and the first line of the body makes it unique.

**Prefer natural boundaries.** Function definitions, if/else blocks, and return
statements are good anchors. Replacing mid-expression is fragile.

**Match the file's actual indentation.** Even with fuzzy matching, it's better
to match exactly. Fuzzy matching is a fallback, not a license to be careless.

**Not be too long.** An `old_string` that spans 50 lines is unwieldy and
increases the chance of small differences (the model writes from memory, not
from a verbatim copy). 5–15 lines is typically the right range.

### 3.8 Error messages

Error messages from `edit_file` are designed to help the model self-correct:

```go
// Not found
fmt.Errorf("edit_file: old_string not found in %s\n\nSearched for:\n%s",
    inp.Path, inp.OldString)

// Ambiguous
fmt.Errorf("edit_file: old_string matches %d locations in %s — " +
    "make it more specific by including surrounding lines", count, inp.Path)

// Empty old_string
fmt.Errorf("edit_file: old_string cannot be empty — " +
    "use write_file to create new files")
```

Each error tells the model:
- What went wrong
- What was being attempted
- How to fix it

The "not found" error includes the searched-for string. This helps the model
compare what it sent against what the file actually contains, often revealing
the discrepancy immediately.

---

## 4. `list_directory`

### 4.1 Output format

```
src/handlers/:
  dir              2024-01-15T14:23  auth
  file    12.4K    2024-01-15T14:23  auth.go
  file     4.2K    2024-01-14T09:11  auth_test.go
  link             2024-01-10T08:00  config -> ../config
```

Four columns: type, size, modified time, name. This format gives the model
the information it needs to decide which files to read next:

**Type** distinguishes files from directories from symlinks, preventing the
model from trying to `read_file` a directory.

**Size** helps the model decide whether to read the whole file or use a line
range. A 2 MB file should be read in sections; a 4 KB file can be read whole.

**Modified time** (truncated to minute precision) helps the model understand
file freshness. Recently modified files are more likely to be relevant to the
current task.

**Name** is last so the other columns align. Files with long names don't push
the metadata columns off the right edge.

### 4.2 Hidden files

`list_directory` shows all entries from `os.ReadDir`, including hidden files
(those starting with `.`). This is different from the default `ls` behaviour
which hides dotfiles. Reason: the model often needs to know about `.gitignore`,
`.env`, `.claude/settings.json`, etc. Hiding them by default would require the
model to use a separate tool or pass a flag.

### 4.3 Size formatting

```go
func formatSize(n int64) string {
    switch {
    case n < 1024:          return fmt.Sprintf("%6dB", n)
    case n < 1024*1024:     return fmt.Sprintf("%5.1fK", float64(n)/1024)
    case n < 1024*1024*1024: return fmt.Sprintf("%5.1fM", float64(n)/(1024*1024))
    default:                return fmt.Sprintf("%5.1fG", float64(n)/(1024*1024*1024))
    }
}
```

Fixed-width 6-character output so the size column aligns. Directories show
6 spaces (no size) to maintain column alignment. Binary prefixes (K, M, G)
because that's what developers expect from `ls -lh`.

---

## 5. `file_info`

### 5.1 Purpose

A lightweight alternative to `read_file` for when the model needs metadata
but not content: does this file exist? How large is it? Is it a directory?
Is it executable?

```
path:     src/main.go
type:     file
size:     4312 bytes
mode:     -rwxr-xr-x
modified: 2024-01-15T14:23:45Z
```

### 5.2 Non-existence handling

```go
info, err := os.Stat(absPath)
if os.IsNotExist(err) {
    return fmt.Sprintf("path %q does not exist", inp.Path), nil
}
```

Non-existence is returned as a normal result, not an error. This is important:
if the model uses `file_info` to check whether a file exists before creating
it, an error response would confuse the model. The string `"path '...' does
not exist"` is unambiguous.

### 5.3 Symlink resolution

```go
if kind == "symlink" {
    if target, err := filepath.EvalSymlinks(absPath); err == nil {
        extra = fmt.Sprintf("\n  target:  %s", target)
    }
}
```

For symlinks, the output includes the resolved target path. This lets the
model understand where a symlink points before deciding whether to follow it.

Note that `os.Stat` follows symlinks (it stats the target, not the link
itself). To detect a symlink, we'd need `os.Lstat`. The current implementation
using `os.Stat` means `kind == "symlink"` never fires in practice. To fix:

```go
// Use Lstat to detect symlinks without following them
info, err := os.Lstat(absPath)
```

This is a bug in the current implementation. The model will see
`type: file` or `type: directory` for symlinks rather than `type: symlink`.
The symlink target is never shown. Since symlinks are relatively rare in
typical codebases, this hasn't caused problems in practice, but it should
be fixed.

---

## 6. Tool Interaction Patterns

Understanding how the model uses these tools together reveals design
requirements that aren't obvious from looking at individual tools.

### 6.1 The read-then-edit pattern

The most common pattern:

```
read_file(path: "src/auth.go")
    → [model reads and understands the file]
edit_file(
    path: "src/auth.go",
    old_string: "func validateToken(token string) bool {\n\treturn len(token) > 0\n}",
    new_string: "func validateToken(token string) (bool, error) {\n\t..."
)
```

The `read_file` output with line numbers helps the model construct the
`old_string` accurately — it can see exactly what's in the file, including
whitespace, and copy it into the `old_string`.

### 6.2 The parallel read pattern

When the model needs to understand multiple files:

```
[Single assistant response with multiple tool calls]
read_file(path: "src/auth.go")          ← runs concurrently
read_file(path: "src/auth_test.go")     ← runs concurrently
read_file(path: "src/user.go")          ← runs concurrently
```

All three reads complete in parallel (via `errgroup` in the agent loop). The
model then has all three files' contents in the next user turn and can reason
about them together.

### 6.3 The explore pattern

When the model doesn't know the project structure:

```
list_directory(path: ".")
    → [sees src/, tests/, go.mod, README.md]
list_directory(path: "src")
    → [sees handlers/, models/, main.go]
read_file(path: "src/main.go")
    → [understands entry point]
```

`list_directory` is much cheaper than `read_file` for exploration. The model
should use it to orient itself before reading files.

### 6.4 The verify pattern

After making changes, the model often re-reads to verify:

```
edit_file(path: "src/auth.go", ...)
    → [diff shows the change]
read_file(path: "src/auth.go", start_line: 45, end_line: 65)
    → [model verifies the surrounding context looks right]
```

The line range on the verification read is efficient — the model only reads
the changed region plus a few lines of context, not the whole file.

---

## 7. Testing Strategy

### `read_file` tests

```go
// Happy path: read a text file
writeTestFile(t, "hello.go", "package main\n\nfunc main() {}\n")
result, err := readTool.Execute(ctx, marshal(`{"path": "hello.go"}`))
assertNoError(t, err)
assertContains(t, result, "package main")
assertContains(t, result, "     1\t")  // line number annotation

// Line range
result, err = readTool.Execute(ctx, marshal(`{"path":"hello.go","start_line":2,"end_line":2}`))
assertNoError(t, err)
assertContains(t, result, "     2\t")
assertNotContains(t, result, "     1\t")
assertNotContains(t, result, "     3\t")

// Binary detection: null byte
writeTestFile(t, "binary.bin", "hello\x00world")
_, err = readTool.Execute(ctx, marshal(`{"path": "binary.bin"}`))
assertError(t, err)
assertContains(t, err.Error(), "binary")

// Binary detection: invalid UTF-8
writeTestFile(t, "latin1.txt", "\xff\xfe")
_, err = readTool.Execute(ctx, marshal(`{"path": "latin1.txt"}`))
assertError(t, err)

// Path traversal
_, err = readTool.Execute(ctx, marshal(`{"path": "../../etc/passwd"}`))
assertError(t, err)
assertContains(t, err.Error(), "escapes")

// Large file truncation
writeTestFile(t, "large.txt", strings.Repeat("x\n", 200_000))
result, err = readTool.Execute(ctx, marshal(`{"path": "large.txt"}`))
assertNoError(t, err)
assert(t, len(result) <= 200_200)  // capped + truncation note
assertContains(t, result, "truncated")
```

### `write_file` tests

```go
// Creates new file
_, err := writeTool.Execute(ctx, marshal(`{"path":"new.txt","content":"hello"}`))
assertNoError(t, err)
content, _ := os.ReadFile(filepath.Join(workDir, "new.txt"))
assert(t, string(content) == "hello")

// Overwrites existing
os.WriteFile(filepath.Join(workDir, "existing.txt"), []byte("old"), 0o644)
_, err = writeTool.Execute(ctx, marshal(`{"path":"existing.txt","content":"new"}`))
content, _ = os.ReadFile(filepath.Join(workDir, "existing.txt"))
assert(t, string(content) == "new")

// Creates parent directories
_, err = writeTool.Execute(ctx, marshal(`{"path":"deep/nested/file.txt","content":"x"}`))
assertNoError(t, err)
assertFileExists(t, filepath.Join(workDir, "deep/nested/file.txt"))

// Preserves executable bit
os.WriteFile(filepath.Join(workDir, "script.sh"), []byte("#!/bin/sh"), 0o755)
writeTool.Execute(ctx, marshal(`{"path":"script.sh","content":"#!/bin/sh\necho hi"}`))
info, _ := os.Stat(filepath.Join(workDir, "script.sh"))
assert(t, info.Mode().Perm() == 0o755)
```

### `edit_file` tests — the most important

```go
// Exact match — happy path
writeTestFile(t, "main.go", "package main\n\nfunc foo() {}\n")
result, err := editTool.Execute(ctx, marshal(`{
    "path": "main.go",
    "old_string": "func foo() {}",
    "new_string": "func foo() { return }"
}`))
assertNoError(t, err)
assertContains(t, result, "-func foo() {}")
assertContains(t, result, "+func foo() { return }")
content, _ := os.ReadFile(filepath.Join(workDir, "main.go"))
assertContains(t, string(content), "func foo() { return }")

// Fuzzy match — trailing spaces
writeTestFile(t, "spaced.go", "func bar() {   \n    return nil   \n}\n")
_, err = editTool.Execute(ctx, marshal(`{
    "path": "spaced.go",
    "old_string": "func bar() {\n    return nil\n}",
    "new_string": "func bar() {\n    return err\n}"
}`))
assertNoError(t, err)
content, _ = os.ReadFile(filepath.Join(workDir, "spaced.go"))
assertContains(t, string(content), "return err")

// Ambiguous exact match
writeTestFile(t, "dup.go", "return nil\n// ...\nreturn nil\n")
_, err = editTool.Execute(ctx, marshal(`{
    "path": "dup.go",
    "old_string": "return nil",
    "new_string": "return err"
}`))
assertError(t, err)
assertContains(t, err.Error(), "2 locations")

// Not found
writeTestFile(t, "notfound.go", "func main() {}\n")
_, err = editTool.Execute(ctx, marshal(`{
    "path": "notfound.go",
    "old_string": "func helper() {}",
    "new_string": "func helper() { return }"
}`))
assertError(t, err)
assertContains(t, err.Error(), "not found")
assertContains(t, err.Error(), "func helper() {}")  // shows what was searched

// Edit at start of file
writeTestFile(t, "start.go", "package main\nfunc main() {}\n")
_, err = editTool.Execute(ctx, marshal(`{
    "path": "start.go",
    "old_string": "package main",
    "new_string": "package main\n\nimport \"fmt\""
}`))
assertNoError(t, err)
content, _ = os.ReadFile(filepath.Join(workDir, "start.go"))
assert(t, strings.HasPrefix(string(content), "package main\n\nimport"))

// Edit at end of file
// Edit entire file content
// Empty new_string (deletion)
// old_string with only whitespace differences from actual file
// old_string that matches after 2 normalisation passes but not 1
```

### Fuzz testing for `edit_file`

The fuzzy matching logic is complex enough to warrant fuzz testing:

```go
func FuzzEditFile(f *testing.F) {
    f.Fuzz(func(t *testing.T, content, oldStr, newStr string) {
        result, err := fuzzyReplace(content, oldStr, newStr)
        if err == nil {
            // Invariant: result should contain newStr
            if !strings.Contains(result, newStr) && newStr != "" {
                t.Errorf("result does not contain newStr")
            }
            // Invariant: result should not contain oldStr (normalised)
            if strings.Contains(normaliseWS(result), normaliseWS(oldStr)) {
                t.Errorf("result still contains oldStr after replacement")
            }
        }
    })
}
```

---

## 8. Edge Cases and Known Issues

### `edit_file`: multi-occurrence in fuzzy mode

The current fuzzy replacement finds the **first** match. If the normalised
`old_string` appears multiple times in the normalised file, we correctly
detect this with `fuzzyCount > 1` and refuse. But the count check uses
`strings.Count(normFile, normOld)` which may miss cases where the sliding
window finds a match that `strings.Count` doesn't (e.g. overlapping patterns).
In practice this doesn't occur with natural source code, but it's a theoretical
gap.

### `edit_file`: CRLF line endings

Files with Windows-style CRLF line endings (`\r\n`) will have the `\r`
preserved in the output of `read_file` (since `strings.Split` splits on `\n`
not `\r\n`). When the model constructs an `old_string` from `read_file` output,
it will include the `\r` if present. Fuzzy matching will normalise it away
(since `unicode.IsSpace` matches `\r`), but the replacement will not restore
the CRLF — the new lines will be LF-only. This silently converts the file's
line endings, which may be undesirable. Future fix: detect CRLF files and
preserve their line endings through the edit.

### `read_file`: the trailing newline split issue

As mentioned in §1.3, files ending with `\n` have an empty final element after
`strings.Split`. When annotating with line numbers, this produces a spurious
empty line N+1. Fix:

```go
lines := strings.Split(content, "\n")
// Remove trailing empty element if file ends with \n
if len(lines) > 0 && lines[len(lines)-1] == "" {
    lines = lines[:len(lines)-1]
}
```

### `file_info`: symlink detection uses `os.Stat` not `os.Lstat`

Documented in §5.3. Fix requires changing `os.Stat` to `os.Lstat`.

### `write_file`: no size limit on content

The `content` field in `writeFileInput` is unbounded. A model could be induced
to write a 100 MB file, consuming significant disk space and context tokens.
A reasonable cap would be `toolutil.MaxOutputBytes` (200 KB) on the write
content, returning an error for larger inputs.

---

## 9. Future Considerations

### `move_file` and `delete_file`

These tools are notably absent. They were omitted because:

- `move_file` can be implemented via `bash: mv` with a permission prompt
- `delete_file` is high-risk and models should be conservative about deletion

If added, both would require `NeedsPermission: true` and careful output
messaging to confirm what was moved/deleted.

### `patch_file`

An alternative to `edit_file` that accepts a unified diff directly:

```go
type patchFileInput struct {
    Path  string `json:"path"`
    Patch string `json:"patch"`  // unified diff format
}
```

This would allow the model to express complex multi-location changes in a
single tool call instead of multiple sequential `edit_file` calls. The
implementation would use a diff-application library. The challenge is error
reporting when the patch doesn't apply cleanly.

### `read_file` with encoding detection

Future support for common non-UTF-8 encodings (Latin-1, UTF-16 BOM) by
detecting the encoding and transcoding to UTF-8 before returning. Requires
the `golang.org/x/text` package.

### `list_directory` with depth control

`list_directory(path: ".", depth: 2)` would return a recursive tree view up
to the specified depth, without the overhead of multiple tool calls. Useful
for understanding project structure at a glance.

---

*Previous: [`03-tools-overview.md`](./03-tools-overview.md)*  
*Next: [`05-shell-search-tools.md`](./05-shell-search-tools.md) — bash, glob, grep*
