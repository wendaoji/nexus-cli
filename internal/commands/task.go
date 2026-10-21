package commands

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/wendaoji/nexus-cli/internal/i18n"
)

const taskWaitUsageEN = `Usage: nexus-cli task wait <taskId> [--timeout seconds] [--interval seconds] [-o json]

Polls an asynchronous task until it reaches a terminal state.
Exit codes: 0 = success, 1 = failed, 124 = timeout.

A Nexus task definition is WAITING while idle, so a run only counts as finished
once RUNNING has been observed and the task returns to idle; lastRunResult then
decides success.
`

func taskWaitUsage() string { return i18n.H("help.task-wait", taskWaitUsageEN) }

// TaskWait implements "nexus-cli task wait".
func TaskWait(args []string) error {
	fs := flag.NewFlagSet("task wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		timeout  int
		interval int
		out      outputFlags
	)
	fs.IntVar(&timeout, "timeout", 300, "timeout in seconds")
	fs.IntVar(&interval, "interval", 3, "poll interval in seconds")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, taskWaitUsage()) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, taskWaitUsage())
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New(i18n.T("usage: nexus-cli task wait <taskId> [--timeout seconds] [--interval seconds]"))
	}
	id := fs.Arg(0)
	if interval < 1 {
		interval = 1
	}

	c, err := newAuthedClient()
	if err != nil {
		return err
	}

	type state struct {
		ID            string `json:"id"`
		CurrentState  string `json:"currentState"`
		LastRunResult string `json:"lastRunResult"`
	}

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	seenRunning := false

	for {
		resp, err := c.Do(context.Background(), http.MethodGet, "/v1/tasks/"+url.PathEscape(id), nil, "")
		if err != nil {
			return fmt.Errorf("%s: %w", i18n.T("read task %s", id), err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s", i18n.T("read task %s failed (HTTP %d): %s", id, resp.StatusCode, truncate(string(body), 200)))
		}
		var st state
		if err := json.Unmarshal(body, &st); err != nil {
			return fmt.Errorf("%s: %w", i18n.T("parse task %s", id), err)
		}

		var (
			done    bool
			success bool
		)
		switch st.CurrentState {
		case "RUNNING", "BLOCKED":
			seenRunning = true
		case "COMPLETED":
			done, success = true, st.LastRunResult == "" || st.LastRunResult == "OK"
		case "FAILED", "CANCELLED":
			done, success = true, false
		case "WAITING", "PENDING":
			if seenRunning {
				done, success = true, st.LastRunResult == "" || st.LastRunResult == "OK"
			}
		}

		if done {
			type result struct {
				ID     string `json:"id"`
				State  string `json:"state"`
				Result string `json:"lastRunResult"`
				OK     bool   `json:"ok"`
			}
			r := result{ID: id, State: st.CurrentState, Result: st.LastRunResult, OK: success}
			if err := emit(out.JSON, r, func(w io.Writer) {
				if success {
					fmt.Fprintln(w, i18n.T("task %s finished (state=%s, lastRunResult=%s)", id, st.CurrentState, orEmpty(st.LastRunResult)))
				} else {
					fmt.Fprintln(w, i18n.T("task %s did not succeed (state=%s, lastRunResult=%s)", id, st.CurrentState, orEmpty(st.LastRunResult)))
				}
			}); err != nil {
				return err
			}
			if !success {
				return fmt.Errorf("%s", i18n.T("task %s did not succeed", id))
			}
			return nil
		}

		if time.Now().After(deadline) {
			return &TimeoutError{Task: id, State: st.CurrentState, Timeout: timeout}
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

// TimeoutError signals that waiting for a task timed out; main maps it to 124.
type TimeoutError struct {
	Task    string
	State   string
	Timeout int
}

func (e *TimeoutError) Error() string {
	return i18n.T("timed out waiting for task %s (%ds), current state %s", e.Task, e.Timeout, e.State)
}

func orEmpty(s string) string {
	if s == "" {
		return i18n.T("<empty>")
	}
	return s
}
