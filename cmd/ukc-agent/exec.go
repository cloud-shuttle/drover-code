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

const (
	maxStreamReplayEvents = 1000
	streamReplayGrace     = 15 * time.Minute
)

type job struct {
	id        string
	mu        sync.Mutex
	baseIndex int
	history   []map[string]any
	done      bool
	listeners []chan struct{}
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
		id:        id,
		history:   make([]map[string]any, 0, 64),
		listeners: make([]chan struct{}, 0),
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
	defer j.scheduleRemoval(jr)

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
}

func (j *job) trySend(m map[string]any) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.history = append(j.history, m)
	for len(j.history) > maxStreamReplayEvents {
		j.history = j.history[1:]
		j.baseIndex++
	}
	if done, _ := m["done"].(bool); done {
		j.done = true
	}
	for _, ch := range j.listeners {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (j *job) scheduleRemoval(jr *jobRunner) {
	time.AfterFunc(streamReplayGrace, func() { jr.remove(j.id) })
}

func (j *job) subscribe() chan struct{} {
	j.mu.Lock()
	defer j.mu.Unlock()
	ch := make(chan struct{}, 1)
	j.listeners = append(j.listeners, ch)
	return ch
}

func (j *job) unsubscribe(ch chan struct{}) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i, c := range j.listeners {
		if c == ch {
			j.listeners = append(j.listeners[:i], j.listeners[i+1:]...)
			break
		}
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return fmt.Errorf("no flush")
	}

	startIndex := -1
	if lastIDStr := r.Header.Get("Last-Event-ID"); lastIDStr != "" {
		var lastID int
		fmt.Sscanf(lastIDStr, "%d", &lastID)
		startIndex = lastID + 1
	}

	ch := j.subscribe()
	defer j.unsubscribe(ch)

	ctx := r.Context()
	gapWarned := false
	for {
		j.mu.Lock()
		base := j.baseIndex
		avail := len(j.history)
		j.mu.Unlock()

		if startIndex < 0 {
			startIndex = base
		}
		if startIndex < base && !gapWarned {
			gapWarned = true
			if err := writeSSE(w, fl, startIndex, map[string]any{
				"stream": "stderr",
				"line":   "stream replay gap: some earlier events were evicted from the worker buffer",
			}); err != nil {
				return err
			}
			startIndex = base
		}

		for idx := startIndex - base; idx >= 0 && idx < avail; idx++ {
			if err := ctx.Err(); err != nil {
				return err
			}

			j.mu.Lock()
			ev := j.history[idx]
			eventID := j.baseIndex + idx
			j.mu.Unlock()

			if err := writeSSE(w, fl, eventID, ev); err != nil {
				return err
			}
			if done, _ := ev["done"].(bool); done {
				return nil
			}
			startIndex = eventID + 1
		}

		j.mu.Lock()
		done := j.done
		j.mu.Unlock()
		if done {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ch:
		}
	}
}

func writeSSE(w http.ResponseWriter, fl http.Flusher, eventID int, ev map[string]any) error {
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\n", eventID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
		return err
	}
	fl.Flush()
	return nil
}
