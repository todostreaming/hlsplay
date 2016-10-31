package mpv

import (
	"bufio"
	"fmt"
	"github.com/isaacml/cmdline"
	"strconv"
	"strings"
	"sync"
	"time"
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
	log     string     // log output
	mu      sync.Mutex // mutex tu protect the internal variables on multithreads
	lastime int64      // last UNIX time a frame was played
	// external config variables
	input   string // input to remux (/var/segments/fifo)
	options string // conformed options string (--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --loop=inf --vd-lavc-software-fallback=no)
	timeout int64  // timeout w/o log if playing (3 seconds)
}

// you dont need to call this func less than secondly
func (m *MPV) Status() *Status {
	var st Status
	var avsync string

	m.mu.Lock()
	defer m.mu.Unlock()

	// Vamos a extraer el avsync del log
	trozos := strings.Fields(m.log)

	for k, v := range trozos {
		switch v {
		case "A-V:":
			avsync = trozos[k+1]
		}
	}

	m.avsync, _ = strconv.ParseFloat(avsync, 64)
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
	mpv.log = ""

	return mpv
}

func (m *MPV) Start() error {
	var err error
	var exe *cmdline.Exec

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	comando := fmt.Sprintf("/usr/bin/mpv %s %s", m.options, m.input)

	go func(mpv *MPV, cmd *cmdline.Exec) {
		for {
			mpv.mu.Lock()
			result := mpv.playing && (time.Now().Unix()-mpv.lastime) > mpv.timeout
			mpv.mu.Unlock()
			if result {
				cmd.Stop()
			}
			time.Sleep(1 * time.Second)
			mpv.mu.Lock()
			if mpv.started == false {
				mpv.mu.Unlock()
				break
			}
			mpv.mu.Unlock()
		}
	}(m, exe)

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
			m.mu.Lock()
			m.lastime = time.Now().Unix()
			m.mu.Unlock()
			line, err := mediareader.ReadString('\n') // blocks until read
			m.mu.Lock()
			if err != nil || m.started == false {
				m.playing = false
				m.ready = false
				m.log = ""
				m.mu.Unlock()
				break
			}
			m.mu.Unlock()
			if strings.Contains(line, "Playing:") {
				m.mu.Lock()
				m.ready = true
				m.mu.Unlock()
			}
			if strings.Contains(line, "AV:") {
				m.mu.Lock()
				m.playing = true
				m.log = strings.TrimRight(line, "\n")
				m.mu.Unlock()
			}
		}
		exe.Stop()
		m.mu.Lock()
		if m.started == false {
			m.mu.Unlock()
			break
		}
		m.mu.Unlock()
	}

	return err
}

func (m *MPV) Stop() error {
	var err error

	m.mu.Lock()
	defer m.mu.Unlock()

	m.started = false

	return err
}