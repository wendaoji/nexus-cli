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
)

const taskWaitUsage = `用法: nexus-cli task wait <taskId> [--timeout 秒] [--interval 秒] [-o json]

轮询异步任务到终态。退出码：0=成功 1=失败 124=超时。

Nexus 的任务定义在空闲时也是 WAITING，所以只有「见过 RUNNING 之后回到空闲」
才算这一轮跑完；再用 lastRunResult 判断成败。
`

// TaskWait 实现 nexus-cli task wait。
func TaskWait(args []string) error {
	fs := flag.NewFlagSet("task wait", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		timeout  int
		interval int
		out      outputFlags
	)
	fs.IntVar(&timeout, "timeout", 300, "超时秒数")
	fs.IntVar(&interval, "interval", 3, "轮询间隔秒数")
	out.addTo(fs)
	fs.Usage = func() { fmt.Fprint(os.Stderr, taskWaitUsage) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(os.Stdout, taskWaitUsage)
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("用法: nexus-cli task wait <taskId> [--timeout 秒] [--interval 秒]")
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
			return fmt.Errorf("读取任务 %s: %w", id, err)
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("读取任务 %s 失败（HTTP %d）: %s", id, resp.StatusCode, truncate(string(body), 200))
		}
		var st state
		if err := json.Unmarshal(body, &st); err != nil {
			return fmt.Errorf("解析任务 %s: %w", id, err)
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
					fmt.Fprintf(w, "任务 %s 已完成（state=%s, lastRunResult=%s）\n", id, st.CurrentState, orEmpty(st.LastRunResult))
				} else {
					fmt.Fprintf(w, "任务 %s 未成功（state=%s, lastRunResult=%s）\n", id, st.CurrentState, orEmpty(st.LastRunResult))
				}
			}); err != nil {
				return err
			}
			if !success {
				return fmt.Errorf("任务 %s 未成功", id)
			}
			return nil
		}

		if time.Now().After(deadline) {
			return &TimeoutError{Task: id, State: st.CurrentState, Timeout: timeout}
		}
		time.Sleep(time.Duration(interval) * time.Second)
	}
}

// TimeoutError 表示等待任务超时；main 会把它映射为退出码 124。
type TimeoutError struct {
	Task    string
	State   string
	Timeout int
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("等待任务 %s 超时（%ds），当前状态 %s", e.Task, e.Timeout, e.State)
}

func orEmpty(s string) string {
	if s == "" {
		return "<空>"
	}
	return s
}
