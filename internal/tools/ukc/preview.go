package ukc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// MaybeConfirmUpload shows a TTY preview and waits for Enter unless skipped.
func MaybeConfirmUpload(stdin *os.File, stdout io.Writer, summary UploadSummary) error {
	if stdin == nil || !isTerminal(stdin) {
		return nil
	}
	fmt.Fprintf(stdout, "Uploading %d files (%.1f MB) to cloud worker. Press Enter to confirm, or Ctrl+C to cancel.\n",
		summary.FileCount, float64(summary.TotalBytes)/(1024*1024))
	sc := bufio.NewScanner(stdin)
	if !sc.Scan() {
		return fmt.Errorf("upload cancelled")
	}
	if strings.TrimSpace(sc.Text()) != "" {
		// Any line content is treated as confirm for ergonomics; only EOF cancels.
	}
	return nil
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
