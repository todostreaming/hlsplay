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
	rmx.WaitforReady()
	err = player.Start()
	if err != nil {
		log.Fatalln("cannot start the player")
	}
	player.WaitforReady()
	err = hls.Run()

	if err != nil {
		log.Fatalln("cannot start the downloader")
	}
	for {
		if rmx.Status().Remuxing {
			fmt.Printf("%s A-V=%.3f\n", rmx.Status().Log, player.Status().AVsync)
		}
		time.Sleep(1 * time.Second)
	}
}
