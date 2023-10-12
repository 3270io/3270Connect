package main

import (
	"log"

	"gitlab.jnnn.gs/jnnngs/go3270"
)

func main() {

	e := go3270.Emulator{
		Host: "10.27.27.62",
		Port: 30050,
	}

	if err := e.Connect(); err != nil {
		log.Fatalf("error to create connection: %v\n", err)
	}

	if err := e.SetString("my_user"); err != nil {
		log.Fatalf("error to set string: %v\n", err)
	}

	if err := e.Press(go3270.Tab); err != nil {
		log.Fatalf("error to press enter: %v\n", err)
	}

	if err := e.SetString("my_password"); err != nil {
		log.Fatalf("error to set string: %v\n", err)
	}

	if err := e.Press(go3270.Enter); err != nil {
		log.Fatalf("error to press enter: %v\n", err)
	}

	v, err := e.GetValue(0, 1, 4)
	if err != nil {
		log.Fatalf("error to get value: %v", err)
	}
	log.Println(v)

	if err := e.Disconnect(); err != nil {
		log.Fatalf("error to disconnect: %v\n", err)
	}

}
