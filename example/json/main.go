package main

import (
	"encoding/json"
	"fmt"
)

type A struct {
	B json.RawMessage
	C string
	D []byte
}

type B struct {
	E string
	F string
}

func main() {
	b := B{
		E: "example",
		F: "example2",
	}
	bytes, _ := json.Marshal(b)

	txt, _ := json.Marshal(A{
		B: bytes,
		C: string(bytes),
		D: bytes,
	})

	fmt.Println(string(txt))
}
