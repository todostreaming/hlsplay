package main

import (
	"fmt"
	"github.com/isaacml/hlsdownload"
	"github.com/todostreaming/hlsplay/mpv"
	"github.com/todostreaming/hlsplay/remux"
	"log"
	"os"
	"time"
)

var Warning *log.Logger

func init() {
	Warning = log.New(os.Stderr, "\n\n[WARNING]: ", log.Ldate|log.Ltime|log.Lshortfile)
}

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no /var/segments/fifo2
func main() {
	hls := hlsdownload.HLSDownloader("http://pablo001.todostreaming.es/radiovida/mobile/playlist.m3u8", "/var/segments/")
	player := mpv.MPVPlayer("/var/segments/fifo2", "--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --vd-lavc-software-fallback=no")
	rmx := remux.Remuxer("/var/segments/fifo", "/var/segments/fifo2")

	err := player.Start()
	if err != nil {
		log.Fatalln("cannot start the player")
	}
	for !player.Status().Ready {
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("MPV Started")
	err = rmx.Start()
	if err != nil {
		log.Fatalln("cannot start remuxer")
	}
	for !rmx.Status().Ready {
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println("Remux Started")
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
			player.PreStop()
			rmx.PreStop()
			hls.Pause()
			hls.WaitforPaused()
			fmt.Println("FIFO Empty !!!")
			time.Sleep(1 * time.Minute)
			break
		}
	}
}

// Could not get DISPMANX objects  (mpv)
