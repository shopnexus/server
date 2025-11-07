package main

import (
	"encoding/json"
	"fmt"

	commonmodel "shopnexus-remastered/internal/module/common/model"
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
	data, _ := json.Marshal(x)
	fmt.Println(string(data)) // "custom:42"

	data, _ = json.Marshal(CommonResponse{
		Data: MyInt(123),
	})
	fmt.Println(string(data)) // {"data":"custom:123"
}
