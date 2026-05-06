package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"sync"
	"time"
)

type job struct {
	id     string
	events chan map[string]any
}

type jobRunner struct {
	mu   sync.Mutex
	jobs map[string]*job
}

func newJobRunner(_ string) *jobRunner {
	return &jobRunner{jobs: make(map[string]*job)}
}

func (jr *jobRunner) start(parent context.Context, command string) string {
	id := randomID()
	j := &job{
		id:     id,
		events: make(chan map[string]any, 128),
	}
	jr.mu.Lock()
	jr.jobs[id] = j
	jr.mu.Unlock()

	go jr.runCommand(parent, j, command)
	return id
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (jr *jobRunner) runCommand(parent context.Context, j *job, command string) {
	// If nobody ever opens /exec/:id/stream, reclaim the map entry eventually.
	time.AfterFunc(10*time.Minute, func() { jr.remove(j.id) })

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = "/workspace"
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		j.emitThenClose(err.Error(), 1)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		j.emitThenClose(err.Error(), 1)
		return
	}

	if err := cmd.Start(); err != nil {
		j.emitThenClose(err.Error(), 1)
		return
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		jr.pipeLines(j, "stdout", stdout)
	}()
	go func() {
		defer wg.Done()
		jr.pipeLines(j, "stderr", stderr)
	}()
	wg.Wait()

	waitErr := cmd.Wait()
	code := 0
	if waitErr != nil {
		if x, ok := waitErr.(*exec.ExitError); ok {
			code = x.ExitCode()
		} else {
			code = 1
		}
	}

	j.emitFinal(code)
}

func (j *job) emitThenClose(errMsg string, code int) {
	j.trySend(map[string]any{"stream": "stderr", "line": errMsg})
	j.emitFinal(code)
}

func (j *job) emitFinal(code int) {
	j.trySend(map[string]any{"done": true, "exit_code": code})
	close(j.events)
}

func (j *job) trySend(m map[string]any) {
	select {
	case j.events <- m:
	default:
	}
}

func (jr *jobRunner) pipeLines(j *job, stream string, r io.Reader) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		j.trySend(map[string]any{"stream": stream, "line": sc.Text()})
	}
	if err := sc.Err(); err != nil {
		j.trySend(map[string]any{"stream": "stderr", "line": err.Error()})
	}
}

func (jr *jobRunner) remove(id string) {
	jr.mu.Lock()
	delete(jr.jobs, id)
	jr.mu.Unlock()
}

func (jr *jobRunner) streamSSE(w http.ResponseWriter, r *http.Request, jobID string) error {
	jr.mu.Lock()
	j, ok := jr.jobs[jobID]
	jr.mu.Unlock()
	if !ok {
		http.Error(w, "unknown job_id", http.StatusNotFound)
		return fmt.Errorf("unknown job")
	}
	defer jr.remove(jobID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return fmt.Errorf("no flush")
	}

	ctx := r.Context()
	for ev := range j.events {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return err
		}
		fl.Flush()
		if done, _ := ev["done"].(bool); done {
			return nil
		}
	}
	return nil
}
