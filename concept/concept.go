package main

import (
	"fmt"
	"github.com/isaacml/hlsdownload"
	"github.com/todostreaming/hlsplay/mpv"
	"github.com/todostreaming/hlsplay/remux"
	"log"
	"os"
	//	"os/exec"
	"time"
)

var Warning *log.Logger

func init() {
	Warning = log.New(os.Stderr, "\n\n[WARNING]: ", log.Ldate|log.Ltime|log.Lshortfile)
}

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no /var/segments/fifo2
func main() {
	hls := hlsdownload.HLSDownloader("http://pablo001.todostreaming.es/radiovida/mobile/playlist.m3u8", "/var/segments/")
	player := mpv.MPVPlayer("/var/segments/fifo2", "--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --vd-lavc-software-fallback=no", 3)
	rmx := remux.Remuxer("/var/segments/fifo", "/var/segments/fifo2", 3)

	err := rmx.Start()
	if err != nil {
		log.Fatalln("cannot start remuxer")
	}
	fmt.Println("Remuxer launched...")
	err = player.Start()
	if err != nil {
		log.Fatalln("cannot start the player")
	}
	fmt.Println("MPV launched...")
	err = hls.Run()
	if err != nil {
		log.Fatalln("cannot start the downloader")
	}
	fmt.Println("Starting Download...")
	done := false
	var t time.Time
	for {
		if !done {
			t = time.Now()
			done = true
		}
		if rmx.Status().Remuxing {
			fmt.Printf("%s A-V=%.3f\n", rmx.Status().Log, player.Status().AVsync)
		}
		time.Sleep(1 * time.Second)
		if time.Since(t).Seconds() > 60.0 {
			done = false
			hls.Pause()
			hls.WaitforPaused()
			rmx.Stop()
			player.Stop()
			Warning.Println("remux resynced")
			time.Sleep(2 * time.Second)
			rmx.Start()
			player.Start()
			hls.Resume()
		}
	}
}
