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
	"strings"
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

type focus struct {
	key         bool
	windowID    string
	workspaceID string
}

// currentFocus is read when the event stream (re)connects. It asks macOS
// whether cmux is in front, since window.list can still report the previous
// focus for over a second after a window.keyed or window.unkeyed event.
func currentFocus() (focus, error) {
	front, err := frontmostBundleID()
	if err != nil || front != cmuxBundleID {
		return focus{}, err
	}
	var windows struct {
		Windows []struct {
			ID                  string `json:"id"`
			Key                 bool   `json:"key"`
			SelectedWorkspaceID string `json:"selected_workspace_id"`
		} `json:"windows"`
	}
	if err := rpc("window.list", nil, &windows); err != nil {
		return focus{}, err
	}
	if len(windows.Windows) == 0 {
		return focus{}, nil
	}
	w := windows.Windows[0]
	for _, candidate := range windows.Windows {
		if candidate.Key {
			w = candidate
		}
	}
	return focus{key: true, windowID: w.ID, workspaceID: w.SelectedWorkspaceID}, nil
}

const cmuxBundleID = "com.cmuxterm.app"

var bundleIDLine = regexp.MustCompile(`bundleID="([^"]+)"`)

func frontmostBundleID() (string, error) {
	asn, err := exec.Command("lsappinfo", "front").Output()
	if err != nil {
		return "", err
	}
	out, err := exec.Command("lsappinfo", "info", "-only", "bundleid", strings.TrimSpace(string(asn))).Output()
	if err != nil {
		return "", err
	}
	m := bundleIDLine.FindSubmatch(out)
	if m == nil {
		return "", errors.New("no bundleID in lsappinfo output")
	}
	return string(m[1]), nil
}

func workspaceTitle(f focus) (string, error) {
	var list struct {
		Workspaces []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"workspaces"`
	}
	if err := rpc("workspace.list", map[string]string{"window_id": f.windowID}, &list); err != nil {
		return "", err
	}
	for _, ws := range list.Workspaces {
		if ws.ID == f.workspaceID {
			return ws.Title, nil
		}
	}
	return "", fmt.Errorf("workspace %s not in window %s", f.workspaceID, f.windowID)
}

type cmuxEvent struct {
	Name    string `json:"name"`
	Payload struct {
		IsKeyWindow bool   `json:"is_key_window"`
		WindowID    string `json:"window_id"`
		WorkspaceID string `json:"workspace_id"`
	} `json:"payload"`
}

// connected is sent each time the event stream starts, before its first event.
const connected = "connected"

func watchFocus(events chan<- cmuxEvent) {
	for {
		cmd := exec.Command(cmuxPath(), "events", "--reconnect", "--no-ack", "--no-heartbeat",
			"--name", "window.keyed", "--name", "window.unkeyed", "--name", "workspace.selected")
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			events <- cmuxEvent{Name: connected}
			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 64*1024), 64*1024)
			for scanner.Scan() {
				var ev cmuxEvent
				if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
					log.Printf("cmux event: %v", err)
					continue
				}
				events <- ev
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
