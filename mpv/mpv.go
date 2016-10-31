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
	playing bool       // playing frames at this moment
	avsync  bool       // DTS difference between Audio and Video packets
	mu      sync.Mutex // mutex tu protect the internal variables on multithreads
	// external config variables
	input   string // input to remux (/var/segments/fifo)
	options string // conformed options string (--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --loop=inf --vd-lavc-software-fallback=no)
}
