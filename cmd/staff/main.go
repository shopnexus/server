// Command staff manages staff members from a host with database access. It
// is the only way to create, disable, enable or reset an admin; it manages
// moderators too.
//
//	go run ./cmd/staff create-admin --email a@example.com --name "An Nguyen"
//	go run ./cmd/staff disable --email a@example.com
//
// The commands are create-admin, disable, enable, reset-password and
// reset-totp. create-admin and reset-password print the temporary password.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/user"
	"slices"
	"time"

	"github.com/shopnexus/server2/internal/platform/audit"
	"github.com/shopnexus/server2/internal/platform/config"
	"github.com/shopnexus/server2/internal/platform/postgres"
	"github.com/shopnexus/server2/internal/staff"
	"github.com/shopnexus/server2/internal/staff/member"
)

const usage = "usage: staff create-admin --email <email> --name <name>\n" +
	"       staff disable|enable|reset-password|reset-totp --email <email>"

const createAdmin = "create-admin"

var commands = []string{createAdmin, "disable", "enable", "reset-password", "reset-totp"}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "staff:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || !slices.Contains(commands, args[0]) {
		return errors.New(usage)
	}
	cmd := args[0]
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	email := fs.String("email", "", "the member's email")
	name := fs.String("name", "", "the display name, for create-admin")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w\n%s", err, usage)
	}
	switch {
	case *email == "":
		return errors.New("--email is required")
	case cmd == createAdmin && *name == "":
		return errors.New("--name is required for create-admin")
	default:
	}

	osUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("current OS user: %w", err)
	}
	if err = config.LoadDotEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return fmt.Errorf("config:\n%w", err)
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	keys, err := staff.NewKeys(cfg.Staff, cfg.Auth, staff.ProductionHasher)
	if err != nil {
		return fmt.Errorf("staff keys: %w", err)
	}
	st := staff.NewStores(pool, keys)
	by, now := audit.ByCLI(osUser.Username), time.Now()

	if cmd == createAdmin {
		v, temp, createErr := staff.CreateMember(ctx, st.Members, keys.Hasher, *email, *name, member.RoleAdmin, by, now)
		if createErr != nil {
			return fmt.Errorf("create admin: %w", createErr)
		}
		if _, printErr := fmt.Fprintf(out, "created admin %s (%s)\ntemporary password: %s\n", v.Email, v.ID, temp); printErr != nil {
			return fmt.Errorf("print the temporary password: %w", printErr)
		}
		return nil
	}
	m, err := st.Members.FindByEmail(ctx, member.NormalizeEmail(*email))
	if errors.Is(err, member.ErrNotFound) {
		return fmt.Errorf("no staff member has the email %s", *email)
	}
	if err != nil {
		return fmt.Errorf("find staff member: %w", err)
	}
	switch cmd {
	case "disable":
		_, err = staff.DisableMember(ctx, st, m.ID(), by, now)
	case "enable":
		_, err = staff.EnableMember(ctx, st, m.ID(), by, now)
	case "reset-password":
		var temp string
		if temp, err = staff.ResetMemberPassword(ctx, st, keys, m.ID(), by, now); err == nil {
			_, err = fmt.Fprintf(out, "temporary password: %s\n", temp)
		}
	default: // reset-totp
		_, err = staff.ResetMemberTOTP(ctx, st, m.ID(), by, now)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	if _, err = fmt.Fprintf(out, "%s %s: done\n", cmd, m.Email()); err != nil {
		return fmt.Errorf("print the result: %w", err)
	}
	return nil
}
