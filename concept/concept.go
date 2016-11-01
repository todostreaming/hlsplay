package main

import (
	"fmt"
	"github.com/isaacml/hlsdownload"
	"github.com/todostreaming/hlsplay/mpv"
	"github.com/todostreaming/hlsplay/remux"
	"log"
	"time"
)

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no /var/segments/fifo2
func main() {
	hls := hlsdownload.HLSDownloader("http://pablo001.todostreaming.es/radiovida/mobile/playlist.m3u8", "/var/segments/")
	player := mpv.MPVPlayer("/var/segments/fifo2", "--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no", 3)
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
	t := time.Now()
	for {
		if rmx.Status().Remuxing {
			fmt.Printf("%s A-V=%.3f\n", rmx.Status().Log, player.Status().AVsync)
		}
		time.Sleep(1 * time.Second)
		if time.Since(t).Seconds() > 60.0 {
			break
		}
	}
	hls.Pause()
	fmt.Println("Pausing Download...")
	hls.WaitforPaused()
	fmt.Println("Download paused")
	rmx.Stop()
	fmt.Println("Remuxer stopped")
	player.Stop()
	fmt.Println("Player stopped")
	time.Sleep(1 * time.Minute)
	fmt.Println("Waiting for 1 minute...")
	rmx.Start()
	fmt.Println("Remuxer launched...")
	player.Start()
	fmt.Println("Player launched...")
	time.Sleep(1 * time.Second)
	hls.Resume()
	fmt.Println("Secuencer resumed")
	for {
		if rmx.Status().Remuxing {
			fmt.Printf("%s A-V=%.3f\n", rmx.Status().Log, player.Status().AVsync)
		}
		time.Sleep(1 * time.Second)
	}
}
