// Command keygen prints fresh OTP and JWT keys as .env lines.
package main

import (
	"fmt"

	"github.com/shopnexus/server2/internal/platform/config"
)

func main() { fmt.Print(config.NewAuthKeys().Env()) }
