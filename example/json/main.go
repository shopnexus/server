package main

import (
	"encoding/json"
	"fmt"

	"github.com/bytedance/sonic"
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
	bytes, _ := sonic.Marshal(b)

	var sl []byte

	txt, _ := sonic.Marshal(A{
		B: bytes,
		C: string(bytes),
		D: sl,
	})

	fmt.Println(string(txt))
}
