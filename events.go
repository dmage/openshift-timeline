package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"
)

type traceEvent struct {
	Name string `json:"name"`
	Ph   string `json:"ph"`
	PID  int    `json:"pid"`
	TID  int    `json:"tid"`
	TS   int64  `json:"ts"`

	Cat  string            `json:"cat,omitempty"`
	Dur  int64             `json:"dur,omitempty"`
	Args map[string]string `json:"args,omitempty"`
}

type traceEventsGenerator struct {
	tidsCount int
	tids      map[string]int
	pids      map[int]bool
	wantComma bool
}

func newTraceEventsGenerator() *traceEventsGenerator {
	return &traceEventsGenerator{
		tidsCount: 0,
		tids:      make(map[string]int),
		pids:      make(map[int]bool),
	}
}

func (g *traceEventsGenerator) GetPID(name string) int {
	tid := g.GetTID(0, name)
	if !g.pids[tid] {
		g.Emit(&traceEvent{
			Name: "process_name",
			Ph:   "M",
			PID:  tid,
			TID:  tid,
			Args: map[string]string{
				"name": name,
			},
		})
		g.pids[tid] = true
	}
	return tid
}

func (g *traceEventsGenerator) GetTID(pid int, name string) int {
	key := fmt.Sprintf("%d:%s", pid, name)
	tid, ok := g.tids[key]
	if !ok {
		g.tidsCount++
		tid = g.tidsCount
		if pid == 0 {
			pid = tid
		}
		g.tids[key] = tid
		g.Emit(&traceEvent{
			Name: "thread_name",
			Ph:   "M",
			PID:  pid,
			TID:  tid,
			Args: map[string]string{
				"name": name,
			},
		})
	}
	return tid
}

func (g *traceEventsGenerator) Emit(event *traceEvent) {
	if g.wantComma {
		fmt.Print(",")
	} else {
		g.wantComma = true
	}
	buf, err := json.Marshal(event)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s\n", buf)
}

func isAbnormal(condition, status string) bool {
	switch condition {
	case "condition/Available":
		return strings.HasPrefix(status, "status/False")
	}
	return strings.HasPrefix(status, "status/True")
}

func main() {
	f := os.Stdin

	r := bufio.NewReader(f)
	fmt.Printf("[")
	g := newTraceEventsGenerator()
	pid1 := g.GetPID("other")
	inProgress := map[string]string{}
	for {
		line, readErr := r.ReadString('\n')
		if line != "" || readErr == nil {
			line = strings.TrimRight(line, "\n")
			if line == "" {
				continue
			}

			if len(line) < 20 || line[19] != ' ' {
				log.Fatalf("unexpected line %q", line)
			}
			timestamp, err := time.Parse("2006 Jan 02 15:04:05", "2021 "+line[:19])
			if err != nil {
				log.Fatal(err)
			}

			var duration time.Duration
			pid := pid1
			tid := 0
			phase := "X"

			msg := line[20:]
			if msg[0] == '-' {
				msg = msg[2:]
				idx := strings.Index(msg, " ")
				duration, err = time.ParseDuration(msg[:idx])
				if err != nil {
					log.Fatal(err)
				}
				msg = strings.TrimLeft(msg[idx+1:], " ")
			}

			msg = msg[2:] // I, W or E

			parts := strings.SplitN(msg, " ", 2)
			if strings.HasPrefix(parts[0], "ns/e2e-") {
				pid = g.GetPID("ns/e2e-*")
			} else if strings.HasPrefix(parts[0], "ns/") {
				pid = g.GetPID(parts[0])

				parts = strings.SplitN(parts[1], " ", 2)
				if strings.HasPrefix(parts[0], "pod/") {
					tid = g.GetTID(pid, parts[0])
				}
			} else if strings.HasPrefix(parts[0], "node/") {
				pid = g.GetPID(parts[0])
			} else if strings.HasPrefix(parts[0], "clusteroperator/") {
				pid = g.GetPID(parts[0])

				parts = strings.SplitN(parts[1], " ", 2)
				if strings.HasPrefix(parts[0], "condition/") {
					tid = g.GetTID(pid, parts[0])

					key := fmt.Sprintf("%s %s", pid, tid)
					if previous, ok := inProgress[key]; ok {
						g.Emit(&traceEvent{
							Name: previous,
							Cat:  "",
							Ph:   "E",
							PID:  pid,
							TID:  tid,
							TS:   timestamp.UnixNano() / 1e3,
						})
						delete(inProgress, key)
					}
					if isAbnormal(parts[0], parts[1]) {
						phase = "B"
						inProgress[key] = msg
					}
				}
			} else if strings.HasPrefix(parts[0], "e2e-test/") {
				pid = g.GetPID("e2e-test/*")
				test := ""
				started := false
				if strings.HasSuffix(msg, " started") {
					test = strings.TrimSuffix(msg, " started")
					tid = g.GetTID(pid, test)
					started = true
				} else if idx := strings.Index(msg, " finishedStatus/"); idx != -1 {
					test = msg[:idx]
					tid = g.GetTID(pid, test)
				}
				if tid != 0 {
					key := fmt.Sprintf("%s %s", pid, tid)
					if previous, ok := inProgress[key]; ok {
						g.Emit(&traceEvent{
							Name: previous,
							Cat:  "",
							Ph:   "E",
							PID:  pid,
							TID:  tid,
							TS:   timestamp.UnixNano() / 1e3,
						})
						delete(inProgress, key)
					}
					if started {
						msg = test
						phase = "B"
						inProgress[key] = msg
					}
				}
			}
			if tid == 0 {
				tid = pid
			}

			if duration < time.Second {
				duration = time.Second
			}

			g.Emit(&traceEvent{
				Name: msg,
				Cat:  "",
				Ph:   phase,
				PID:  pid,
				TID:  tid,
				TS:   timestamp.UnixNano() / 1e3,
				Dur:  int64(duration) / 1e3,
			})
		}
		if readErr == io.EOF {
			break
		} else if readErr != nil {
			log.Fatal(readErr)
		}
	}
	fmt.Printf("]\n")
}
