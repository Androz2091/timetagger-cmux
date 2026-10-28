# timetagger-cmux

Tracks the time you spend in each [cmux](https://cmux.com) workspace in [TimeTagger](https://timetagger.app).

A record runs only while a cmux window has keyboard focus and you have touched the keyboard or mouse in the last 5 minutes. Each workspace gets its own record, described `#cmux #<workspace title>`. macOS only.

```sh
go install github.com/Androz2091/timetagger-cmux@latest

TIMETAGGER_API=https://timetagger.app/api/v2 \
TIMETAGGER_TOKEN=<API token from your TimeTagger account page> \
timetagger-cmux
```

A self-hosted instance serves its API at `https://<host>/timetagger/api/v2`. `timetagger-cmux -h` lists the flags: idle timeout, update interval, minimum record length, and `-panes`, which keeps a record per focused pane in a split workspace, described by the pane's title, rather than one per workspace.
