package main

import (
	"fmt"
	"github.com/isaacml/hlsdownload"
	"github.com/todostreaming/hlsplay/mpv"
	"github.com/todostreaming/hlsplay/remux"
	"log"
	"os"
	"runtime"
	"time"
)

var Warning *log.Logger

func init() {
	Warning = log.New(os.Stderr, "\n\n[WARNING]: ", log.Ldate|log.Ltime|log.Lshortfile)
}

// mpv --vo=opengl --rpi-layer=0 --rpi-background=yes --audio-device=alsa/plughw:0,0 --video-aspect 16:9 --vd-lavc-software-fallback=no --deinterlace=yes /var/segments/fifo2
func main() {
	hls := hlsdownload.HLSDownloader("http://edge3.adnstream.com:81/hls/intereconomia.m3u8", "/var/segments/")
	player := mpv.MPVPlayer("/var/segments/fifo2", "--vo=opengl --rpi-layer=0 --rpi-background=yes --audio-device=alsa/plughw:0,0 --video-aspect 16:9 --vd-lavc-software-fallback=no")
	rmx := remux.Remuxer("/var/segments/fifo", "/var/segments/fifo2")

	err := player.Start()
	if err != nil {
		log.Fatalln("cannot start the player")
	}
	player.WaitforReady()
	fmt.Println("MPV Started")
	err = rmx.Start()
	if err != nil {
		log.Fatalln("cannot start remuxer")
	}
	rmx.WaitforReady()
	fmt.Println("Remux Started")
	err = hls.Run()
	if err != nil {
		log.Fatalln("cannot start the downloader")
	}
	fmt.Println("Starting Download...")
	done := false
	var t time.Time
	count := 0
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
			count++
			done = false
			player.PreStop()
			rmx.PreStop()
			hls.Pause()
			fmt.Println("Pausing Download...")
			hls.WaitforPaused() // blocks until completely paused
			fmt.Println("Stopping player")
			player.Stop()
			player.WaitforStopped() // blocks until stopped
			fmt.Println("Stopping remuxer")
			rmx.Stop()
			rmx.WaitforStopped()
			fmt.Printf("FIFO Empty [%d] Goroutines = %d!!!\n", count, runtime.NumGoroutine())

			err := player.Start()
			if err != nil {
				log.Fatalln("cannot start the player")
			}
			player.WaitforReady()
			fmt.Println("MPV Started")
			err = rmx.Start()
			if err != nil {
				log.Fatalln("cannot start remuxer")
			}
			rmx.WaitforReady()
			fmt.Println("Remux Started")
			hls.Resume()
			fmt.Println("Resume Download...")
		}
	}
}

// Could not get DISPMANX objects  (mpv)
