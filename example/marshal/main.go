package main

import (
	"fmt"

	commonmodel "shopnexus-remastered/internal/shared/model"

	"github.com/bytedance/sonic"
)

type CommonResponse struct {
	Data  any               `json:"data,omitempty"`
	Error commonmodel.Error `json:"error,omitempty"`
}

type MyInt int

func (m MyInt) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("\"custom:%d\"", m)), nil
}

func main() {
	var x commonmodel.Concurrency = 4212312312123123
	data, _ := sonic.Marshal(x)
	fmt.Println(string(data)) // "custom:42"

	data, _ = sonic.Marshal(CommonResponse{
		Data: MyInt(123),
	})
	fmt.Println(string(data)) // {"data":"custom:123"
}
