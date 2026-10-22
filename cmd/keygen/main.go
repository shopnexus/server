// Command keygen prints fresh OTP and JWT keys as .env lines. With -restate
// it makes sure the Restate request identity key exists at that path, and
// prints its public key as a .env line.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

func main() {
	restatePath := flag.String("restate", "", "path of the Restate identity key, created when missing")
	flag.Parse()
	if *restatePath == "" {
		fmt.Print(config.NewAuthKeys().Env())
		return
	}
	key, err := restatex.EnsureIdentity(*restatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "keygen:", err)
		os.Exit(1)
	}
	fmt.Println("RESTATE_IDENTITY_KEY=" + key)
}
