package main

import (
	"fmt"

	"github.com/bytedance/sonic"
)

type A struct {
	B
}

type B struct {
	E string
	F string
}

func main() {

	txt, _ := sonic.Marshal(A{})

	fmt.Println(string(txt))
}
