// Command timetagger-cmux records the time you spend in each cmux workspace
// to TimeTagger, counting only while a cmux window has keyboard focus and you
// have touched the keyboard or mouse recently.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)

func main() {
	idle := flag.Duration("idle", 5*time.Minute, "stop counting after this long without keyboard or mouse input")
	interval := flag.Duration("interval", 10*time.Second, "how often to check for idle time and update the running record")
	minimum := flag.Duration("min", 10*time.Second, "drop records shorter than this")
	flag.Parse()

	api, token := os.Getenv("TIMETAGGER_API"), os.Getenv("TIMETAGGER_TOKEN")
	if api == "" || token == "" {
		log.Fatal("set TIMETAGGER_API (for eg. https://timetagger.app/api/v2) and TIMETAGGER_TOKEN")
	}

	t := tracker{
		tt:      &timetagger{api: api, token: token, http: http.Client{Timeout: 15 * time.Second}},
		idle:    *idle,
		minimum: int64(minimum.Seconds()),
		maxGap:  3 * *interval,
		unsent:  map[string]record{},
	}
	events := make(chan cmuxEvent, 64)
	go watchFocus(events)
	ticker := time.NewTicker(*interval)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	for {
		select {
		case ev := <-events:
			t.apply(ev)
			t.update(time.Now())
		case <-ticker.C:
			t.update(time.Now())
		case <-signals:
			t.stop(time.Now())
			t.flush()
			return
		}
	}
}

type tracker struct {
	tt        *timetagger
	idle      time.Duration
	minimum   int64
	maxGap    time.Duration
	focus     focus
	current   *record
	sent      bool
	workspace string
	lastCheck time.Time
	unsent    map[string]record
}

func (t *tracker) apply(ev cmuxEvent) {
	switch ev.Name {
	case connected:
		f, err := currentFocus()
		if err != nil {
			log.Print(err)
		}
		t.focus = f
	case "window.keyed":
		t.focus = focus{key: true, windowID: ev.Payload.WindowID, workspaceID: ev.Payload.WorkspaceID}
	case "window.unkeyed":
		if ev.Payload.WindowID == t.focus.windowID {
			t.focus.key = false
		}
	case "workspace.selected":
		t.focus.workspaceID = ev.Payload.WorkspaceID
	}
}

func (t *tracker) update(now time.Time) {
	if !t.lastCheck.IsZero() && now.Sub(t.lastCheck) > t.maxGap {
		// The Mac slept, so the record ends at the last check before it did.
		t.stop(t.lastCheck)
	}
	t.lastCheck = now

	workspace := ""
	if t.focus.key {
		workspace = t.focus.workspaceID
	}
	idle, err := idleTime()
	if err != nil {
		log.Print(err)
		workspace = ""
	}
	if idle >= t.idle {
		t.stop(now.Add(-idle))
		workspace = ""
	}
	if t.current != nil && workspace != t.workspace {
		t.stop(now)
	}
	if workspace != "" && t.current == nil {
		if title, err := workspaceTitle(t.focus); err != nil {
			log.Print(err)
		} else {
			r := newRecord(now, description(title))
			t.current, t.sent, t.workspace = &r, false, workspace
		}
	}
	if t.current != nil {
		t.current.T2 = now.Unix()
		if t.current.T2-t.current.T1 >= t.minimum {
			if !t.sent {
				log.Printf("start %q", t.current.DS)
			}
			t.unsent[t.current.Key], t.sent = *t.current, true
		}
	}
	t.flush()
}

func (t *tracker) stop(end time.Time) {
	if t.current == nil {
		return
	}
	r := *t.current
	t.current, t.workspace = nil, ""
	r.T2 = end.Unix()
	if r.T2-r.T1 < t.minimum {
		if !t.sent {
			return
		}
		r.T2 = max(r.T2, r.T1+1)
		r.DS = "HIDDEN " + r.DS
	}
	log.Printf("stop %q after %s", r.DS, time.Duration(r.T2-r.T1)*time.Second)
	t.unsent[r.Key] = r
}

func (t *tracker) flush() {
	if len(t.unsent) == 0 {
		return
	}
	records := make([]record, 0, len(t.unsent))
	for _, r := range t.unsent {
		records = append(records, r)
	}
	if err := t.tt.put(records...); err != nil {
		log.Print(err)
		return
	}
	clear(t.unsent)
}

// TimeTagger tags are letters, digits, "-", "_" and "/".
var tagChars = regexp.MustCompile(`^[\p{L}\p{N}_/-]+$`)

func description(workspace string) string {
	if tagChars.MatchString(workspace) {
		return "#cmux #" + workspace
	}
	return "#cmux " + workspace
}
