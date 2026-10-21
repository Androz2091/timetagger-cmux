package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strconv"
	"time"
)

const appBundleCLI = "/Applications/cmux.app/Contents/Resources/bin/cmux"

func cmuxPath() string {
	if p, err := exec.LookPath("cmux"); err == nil {
		return p
	}
	return appBundleCLI
}

func rpc(method string, params map[string]string, out any) error {
	args := []string{"rpc", method}
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		args = append(args, string(b))
	}
	b, err := exec.Command(cmuxPath(), args...).Output()
	if err != nil {
		return fmt.Errorf("cmux rpc %s: %w", method, err)
	}
	return json.Unmarshal(b, out)
}

// focusedWorkspace returns the title of the selected workspace in cmux's key
// window, or "" when no cmux window has keyboard focus.
func focusedWorkspace() (string, error) {
	var windows struct {
		Windows []struct {
			ID                  string `json:"id"`
			Key                 bool   `json:"key"`
			SelectedWorkspaceID string `json:"selected_workspace_id"`
		} `json:"windows"`
	}
	if err := rpc("window.list", nil, &windows); err != nil {
		return "", err
	}
	for _, w := range windows.Windows {
		if !w.Key {
			continue
		}
		var list struct {
			Workspaces []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"workspaces"`
		}
		if err := rpc("workspace.list", map[string]string{"window_id": w.ID}, &list); err != nil {
			return "", err
		}
		for _, ws := range list.Workspaces {
			if ws.ID == w.SelectedWorkspaceID {
				return ws.Title, nil
			}
		}
	}
	return "", nil
}

// watchFocus signals on changed whenever cmux reports a window gaining or
// losing focus or a workspace being selected.
func watchFocus(changed chan<- struct{}) {
	for {
		cmd := exec.Command(cmuxPath(), "events", "--reconnect", "--no-ack", "--no-heartbeat",
			"--name", "window.keyed", "--name", "window.unkeyed", "--name", "workspace.selected")
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 64*1024), 64*1024)
			for scanner.Scan() {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
			err = cmd.Wait()
		}
		log.Printf("cmux events stopped: %v", err)
		time.Sleep(5 * time.Second)
	}
}

var hidIdleTime = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)

// idleTime is how long since the last keyboard, mouse or trackpad input.
func idleTime() (time.Duration, error) {
	out, err := exec.Command("ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return 0, err
	}
	m := hidIdleTime.FindSubmatch(out)
	if m == nil {
		return 0, errors.New("HIDIdleTime not found in ioreg output")
	}
	ns, err := strconv.ParseInt(string(m[1]), 10, 64)
	return time.Duration(ns), err
}
