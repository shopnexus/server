package main

import (
	"fmt"
	"shopnexus-remastered/internal/db"
)

func main() {
	noEscape()
}

func noEscape() *db.ListCatalogCommentParams {
	s := db.New(nil)
	arg := db.ListCatalogCommentParams{}
	fmt.Println("HLOO NIGGA", arg)
	//s.ListCatalogComment(context.TODO(), &arg)
	arg.ID = []int64{1}
	fmt.Println(arg)
	_ = s
	return &arg
}
