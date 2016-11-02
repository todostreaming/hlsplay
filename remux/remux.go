package remux

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
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
	started  bool          // Just called Start()=true or Stop()=false
	stop     bool          // order to stop
	ready    bool          // Ready and waiting to receive data from HLSDownloader
	remuxing bool          // remuxing frames at this moment
	lastime  int64         // last UNIX time a frame was remuxed
	log      string        // logging from remuxer
	mu       sync.Mutex    // mutex tu protect the internal variables on multithreads
	writer   *bufio.Writer // write to the cmdline stdin
	// external config variables
	input  string // input to remux (/var/segments/fifo)
	output string // output remuxed	(/var/segments/fifo2)
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

func Remuxer(input, output string) *Remux {
	rmx := &Remux{}
	rmx.mu.Lock()
	defer rmx.mu.Unlock()

	// enter the external config variables
	rmx.input = input
	rmx.output = output
	// initialize the internal variables values
	rmx.started = false
	rmx.ready = false
	rmx.remuxing = false
	rmx.lastime = 0
	rmx.stop = false
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
		stdinWrite, err := exe.StdinPipe()
		if err != nil {
			return err
		}
		r.writer = bufio.NewWriter(stdinWrite)
		if err = exe.Start(); err != nil {
			return err
		}
		for {
			r.mu.Lock()
			r.lastime = time.Now().Unix()
			r.mu.Unlock()
			line, err := mediareader.ReadString('\n') // blocks until read
			r.mu.Lock()
			if err != nil {
				r.remuxing = false
				r.ready = false
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
				r.log = strings.TrimSpace(line)
				r.mu.Unlock()
			}
		}
		exe.Stop()
		r.mu.Lock()
		if r.stop {
			r.mu.Unlock()
			break
		}
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.started = false
	r.stop = false
	r.mu.Unlock()

	return err
}

func (r *Remux) Start() error {
	var err error

	r.mu.Lock()
	if r.started {
		defer r.mu.Unlock()
		return fmt.Errorf("remux: ALREADY_RUNNING_ERROR")
	}
	r.mu.Unlock()
	go r.run()

	return err
}

// prepara a Remux para ser parado externamente por completo
func (r *Remux) PreStop() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.remuxing {
		r.stop = true
	}

	return err
}

// para completamente al Remux internamente
func (r *Remux) Stop() error {
	var err error

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.remuxing {
		r.stop = true
		r.writer.WriteByte('q')
		r.writer.Flush()
	} else {
		return fmt.Errorf("remux: NOT_STOP_AVAIL")
	}

	return err
}

// call this func after Stop() before to re-Start()
func (r *Remux) WaitforStopped() error {
	var err error

	for {
		r.mu.Lock()
		stopped := r.started
		r.mu.Unlock()
		if stopped == false {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

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
		time.Sleep(50 * time.Millisecond)
	}

	return err
}
