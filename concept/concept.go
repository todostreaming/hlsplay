package main

import (
	"fmt"
	"github.com/todostreaming/hlsplay/mpv"
	"log"
	"time"
)

// mpv --vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no /var/segments/fifo2
func main() {
	player := mpv.MPVPlayer("/var/segments/fifo2", "--vo=rpi:background=yes --ao=alsa:device=[hw:0,0] --video-aspect 16:9 --loop=inf --vd-lavc-software-fallback=no", 3)
	err := player.Start()
	if err != nil {
		log.Fatalln("cannot start it")
	}
	for {
		if player.Status().Playing {
			fmt.Printf("A-V => %.2f\n", player.Status().AVsync)
		}
		time.Sleep(1 * time.Second)
	}
}
