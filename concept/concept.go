package main

import (
	"github.com/todostreaming/hlsplay/remux"
	"log"
)

func main() {
	rmx := remux.Remuxer("/var/segments/fifo", "/var/segments/fifo2", 3)
	err := rmx.Start()
	if err != nil {
		log.Fatalln("cannot start it")
	}
}
