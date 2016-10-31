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
		str := "=>"
		if rmx.Status().Started {
			str += " Started OK "
		} else {
			str += " Started NO "
		}
		if rmx.Status().Ready {
			str += " Ready OK "
		} else {
			str += " Ready NO "
		}
		fmt.Println(str)
		time.Sleep(1 * time.Second)
	}
}
