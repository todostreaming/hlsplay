package main

import (
	"fmt"
	"github.com/todostreaming/hlsplay/remux"
	"log"
	"time"
)

func main() {
	rmx := remux.Remuxer("/var/segments/fifo", "/var/segments/fifo2", 3)
	err := rmx.Start()
	if err != nil {
		log.Fatalln("cannot start it")
	}
	for {
		fmt.Printf("%s\n", rmx.Status().Log)
		time.Sleep(1 * time.Second)
	}
}
