package main

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type A struct {
	D uuid.UUID `json:"d"`
}

func main() {
	a := A{D: uuid.New()}
	js, _ := json.Marshal(a)
	println(string(js))

	b := map[*uuid.UUID]string{}
	c := uuid.New()
	d := &c
	b[&c] = "example"
	fmt.Println(b[d])
}
