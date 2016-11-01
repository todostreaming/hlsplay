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

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no  /var/segments/fifo2
type Status struct {
	Started bool    // Just called Start()=true or Stop()=false
	Ready   bool    // Ready and waiting to receive data from the remuxer
	Playing bool    // playing frames at this moment
	AVsync  float64 // DTS difference between Audio and Video packets
	Lastime int64   // last UNIX time a frame was displayed
}

type MPV struct {
	// internal status variables
	started bool          // Just called Start()=true or Stop()=false
	stop    bool          // order to stop
	ready   bool          // Ready and waiting to receive data from the remuxer
	playing bool          // playing & displaying frames at this moment
	avsync  float64       // DTS difference between Audio and Video packets
	log     string        // log output
	mu      sync.Mutex    // mutex tu protect the internal variables on multithreads
	writer  *bufio.Writer // write to the cmdline stdin
	lastime int64         // last UNIX time a frame was played
	// external config variables
	input   string // input to remux (/var/segments/fifo)
	options string // conformed options string (--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --loop=inf --vd-lavc-software-fallback=no)
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
	st.Lastime = m.lastime

	return &st
}

func MPVPlayer(input, options string) *MPV {
	mpv := &MPV{}
	mpv.mu.Lock()
	defer mpv.mu.Unlock()

	// enter the external config variables
	mpv.input = input
	mpv.options = options
	// initialize the internal variables values
	mpv.started = false
	mpv.ready = false
	mpv.playing = false
	mpv.avsync = 0.0
	mpv.stop = false
	mpv.log = ""

	return mpv
}

func (m *MPV) run() error {
	var err error

	m.mu.Lock()
	m.started = true
	m.mu.Unlock()
	comando := fmt.Sprintf("/usr/bin/mpv %s %s", m.options, m.input)

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
		m.writer = bufio.NewWriter(stdinWrite)
		if err = exe.Start(); err != nil {
			return err
		}
		for {
			m.mu.Lock()
			m.lastime = time.Now().Unix()
			m.mu.Unlock()
			line, err := mediareader.ReadString('\n') // blocks until read
			m.mu.Lock()
			if err != nil {
				m.playing = false
				m.ready = false
				m.log = ""
				m.mu.Unlock()
				break
			}
			m.mu.Unlock()
			if strings.Contains(line, "Playing:") {
				m.mu.Lock()
				m.playing = false
				m.log = ""
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
		exe.Wait()
		m.mu.Lock()
		if m.stop {
			m.mu.Unlock()
			break
		}
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.started = false
	m.stop = false
	m.mu.Unlock()

	return err
}

func (m *MPV) Start() error {
	var err error

	go m.run()

	return err
}

// prepara a MPV para ser parado externamente por completo
func (m *MPV) PreStop() error {
	var err error

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.playing {
		m.stop = true
	}

	return err
}

// para completamente al MPV internamente
func (m *MPV) Stop() error {
	var err error

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.playing {
		m.stop = true
		m.writer.WriteByte('q')
		m.writer.Flush()
	}

	return err
}

// call this func after Stop() before to re-Start()
func (m *MPV) WaitforStopped() error {
	var err error

	for {
		m.mu.Lock()
		stopped := m.started
		m.mu.Unlock()
		if stopped == false {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return err
}

// call this func after Start()
func (m *MPV) WaitforReady() error {
	var err error

	for {
		m.mu.Lock()
		ready := m.ready
		m.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	return err
}
