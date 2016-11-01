package remux

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
	"runtime"
	"strings"
	"sync"
	"time"
)

// remux -i /var/segments/fifo -c copy -f mpegts /var/segments/fifo2 -y
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
	resync   bool       // resync just re-launching remux cmdline
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
	rmx.resync = false
	rmx.lastime = 0
	rmx.log = ""

	return rmx
}

func (r *Remux) run() error {
	var err error

	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
	comando := fmt.Sprintf("/usr/bin/remux -i %s -c copy -f mpegts %s -y", r.input, r.output)

	for {
		exe := cmdline.Cmdline(comando)
		stderrRead, err := exe.StderrPipe()
		if err != nil {
			return err
		}
		mediareader := bufio.NewReader(stderrRead)
		go func() {
			for {
				r.mu.Lock()
				result := r.remuxing && (time.Now().Unix()-r.lastime) > r.timeout
				result2 := r.resync
				r.mu.Unlock()
				if result || result2 {
					exe.Stop()
					break
				}
				time.Sleep(100 * time.Millisecond)
				r.mu.Lock()
				if r.started == false {
					r.mu.Unlock()
					break
				}
				r.mu.Unlock()
			}
		}()
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
				r.resync = false
				r.log = ""
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

func (r *Remux) Start() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	go r.run()

	return err
}

func (r *Remux) Stop() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	r.started = false

	return err
}

func (r *Remux) Resync() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	r.resync = true

	return err
}

// call this func after Start()
func (r *Remux) WaitforReady() error {
	var err error

	for {
		r.mu.Lock()
		ready := r.ready
		r.mu.Unlock()
		if ready {
			break
		}
		runtime.Gosched()
	}

	return err
}
