package remux

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
	"strings"
	"sync"
	"time"
)

// remux -i /var/segments/fifo -c copy -f mpegts /var/segments/fifo2
type Status struct {
	Started  bool   // Just called Start()=true or Stop()=false
	Ready    bool   // Ready and waiting to receive data from HLSDownloader
	Remuxing bool   // remuxing frames at this moment
	Lastime  int64  // last UNIX time a frame was remuxed
	Log      string // logging from remuxer
}

type Remux struct {
	// internal status variables
	started  bool       // Just called Start()=true or Stop()=false
	ready    bool       // Ready and waiting to receive data from HLSDownloader
	remuxing bool       // remuxing frames at this moment
	lastime  int64      // last UNIX time a frame was remuxed
	log      string     // logging from remuxer
	mu       sync.Mutex // mutex tu protect the internal variables on multithreads
	// external config variables
	input   string // input to remux (/var/segments/fifo)
	output  string // output remuxed	(/var/segments/fifo2)
	timeout int64  // timeout w/o log if remuxing (3 seconds)
}

// you dont need to call this func less than secondly
func (r *Remux) Status() *Status {
	var st Status

	r.mu.Lock()
	defer r.mu.Unlock()

	st.Lastime = r.lastime
	st.Log = r.log
	st.Ready = r.ready
	st.Remuxing = r.remuxing
	st.Started = r.started

	return &st
}

func Remuxer(input, output string, timeout int64) *Remux {
	rmx := &Remux{}
	rmx.mu.Lock()
	defer rmx.mu.Unlock()

	// enter the external config variables
	rmx.input = input
	rmx.output = output
	rmx.timeout = timeout
	// initialize the internal variables values
	rmx.started = false
	rmx.ready = false
	rmx.remuxing = false
	rmx.lastime = 0
	rmx.log = ""

	return rmx
}

func (r *Remux) Start() error {
	var err error
	var exe *cmdline.Exec

	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
	comando := fmt.Sprintf("/usr/bin/remux -i %s -c copy -f mpegts %s", r.input, r.output)

	go func(rmx *Remux, cmd *cmdline.Exec) {
		for {
			rmx.mu.Lock()
			result := rmx.remuxing && (time.Now().Unix()-rmx.lastime) > rmx.timeout
			rmx.mu.Unlock()
			if result {
				cmd.Stop()
			}
			time.Sleep(1 * time.Second)
			rmx.mu.Lock()
			if rmx.started == false {
				rmx.mu.Unlock()
				break
			}
			rmx.mu.Unlock()
		}
	}(r, exe)

	for {
		exe = cmdline.Cmdline(comando)
		stderrRead, err := exe.StderrPipe()
		if err != nil {
			return err
		}
		mediareader := bufio.NewReader(stderrRead)
		if err = exe.Start(); err != nil {
			return err
		}
		for {
			r.mu.Lock()
			r.lastime = time.Now().Unix()
			r.mu.Unlock()
			line, err := mediareader.ReadString('\n') // blocks until read
			r.mu.Lock()
			if err != nil || r.started == false {
				r.remuxing = false
				r.ready = false
				r.mu.Unlock()
				break
			}
			r.mu.Unlock()
			if strings.Contains(line, "libswresample") {
				r.mu.Lock()
				r.ready = true
				r.mu.Unlock()
			}
			if strings.Contains(line, "frame=") {
				r.mu.Lock()
				r.remuxing = true
				r.log = strings.TrimRight(line, "\n")
				r.mu.Unlock()
			}
		}
		exe.Stop()
		r.mu.Lock()
		if r.started == false {
			r.mu.Unlock()
			break
		}
		r.mu.Unlock()
	}

	return err
}

func (r *Remux) Stop() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = false

	return err
}
