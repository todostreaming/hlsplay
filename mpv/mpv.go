package mpv

import (
	"sync"
)

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no  /avr/segments/fifo2
type Status struct {
	Started bool    // Just called Start()=true or Stop()=false
	Ready   bool    // Ready and waiting to receive data from the remuxer
	Playing bool    // playing frames at this moment
	AVsync  float64 // DTS difference between Audio and Video packets
}

type MPV struct {
	// internal status variables
	started bool       // Just called Start()=true or Stop()=false
	ready   bool       // Ready and waiting to receive data from the remuxer
	playing bool       // playing & displaying frames at this moment
	avsync  float64    // DTS difference between Audio and Video packets
	mu      sync.Mutex // mutex tu protect the internal variables on multithreads
	// external config variables
	input   string // input to remux (/var/segments/fifo)
	options string // conformed options string (--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --loop=inf --vd-lavc-software-fallback=no)
	timeout int64  // timeout w/o log if playing (3 seconds)
}

// you dont need to call this func less than secondly
func (m *MPV) Status() *Status {
	var st Status

	m.mu.Lock()
	defer m.mu.Unlock()

	st.AVsync = m.avsync
	st.Playing = m.playing
	st.Ready = m.ready
	st.Started = m.started

	return &st
}

func MPVPlayer(input, options string, timeout int64) *MPV {
	mpv := &MPV{}
	mpv.mu.Lock()
	defer mpv.mu.Unlock()

	// enter the external config variables
	mpv.input = input
	mpv.options = options
	mpv.timeout = timeout
	// initialize the internal variables values
	mpv.started = false
	mpv.ready = false
	mpv.playing = false
	mpv.avsync = 0.0

	return mpv
}
