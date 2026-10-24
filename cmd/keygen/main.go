// Command keygen prints fresh OTP and JWT keys as .env lines. With -staff
// it prints a STAFF_KEY line. With -restate it makes sure the Restate
// request identity key exists at that path, and prints its public key as a
// .env line.
package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"os"

	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/restatex"
)

func main() {
	restatePath := flag.String("restate", "", "path of the Restate identity key, created when missing")
	staff := flag.Bool("staff", false, "print a fresh STAFF_KEY line")
	flag.Parse()
	switch {
	case *staff:
		fmt.Println("STAFF_KEY=" + base64.StdEncoding.EncodeToString(config.NewStaffKey()))
	case *restatePath != "":
		key, err := restatex.EnsureIdentity(*restatePath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "keygen:", err)
			os.Exit(1)
		}
		fmt.Println("RESTATE_IDENTITY_KEY=" + key)
	default:
		fmt.Print(config.NewAuthKeys().Env())
	}
}
