package remux

import (
	"sync"
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
