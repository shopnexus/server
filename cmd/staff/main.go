// Command staff manages staff members from a host with database access. It
// is the only way to create, disable, enable, unlock or reset an admin; it
// manages moderators too.
//
//	go run ./cmd/staff create-admin --email a@example.com --name "An Nguyen"
//	go run ./cmd/staff disable --email a@example.com
//
// The commands are create-admin, disable, enable, unlock, reset-password and
// reset-totp. unlock clears the sign-in lock on the email, which anyone can
// set with five wrong passwords. create-admin and reset-password print the
// temporary password.
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
	staffdomain "github.com/shopnexus/server2/internal/staff/domain"
	staffsvc "github.com/shopnexus/server2/internal/staff/service"
)

const usage = "usage: staff create-admin --email <email> --name <name>\n" +
	"       staff disable|enable|unlock|reset-password|reset-totp --email <email>"

const (
	cmdCreateAdmin   = "create-admin"
	cmdDisable       = "disable"
	cmdEnable        = "enable"
	cmdUnlock        = "unlock"
	cmdResetPassword = "reset-password"
	cmdResetTOTP     = "reset-totp"
)

var commands = []string{cmdCreateAdmin, cmdDisable, cmdEnable, cmdUnlock, cmdResetPassword, cmdResetTOTP}

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
	case cmd == cmdCreateAdmin && *name == "":
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
	keys, err := staffsvc.NewKeys(cfg.Staff, staffdomain.Production)
	if err != nil {
		return fmt.Errorf("staff keys: %w", err)
	}
	svc := staff.NewService(pool, keys, nil, nil, time.Now)
	by := audit.ByCLI(osUser.Username)

	if cmd == cmdCreateAdmin {
		v, temp, createErr := svc.CreateMember(ctx, staffsvc.CreateMemberInput{Email: *email, DisplayName: *name, Role: staffdomain.RoleAdmin, By: by})
		if createErr != nil {
			return fmt.Errorf("create admin: %w", createErr)
		}
		if _, printErr := fmt.Fprintf(out, "created admin %s (%s)\ntemporary password: %s\n", v.Email, v.ID, temp); printErr != nil {
			return fmt.Errorf("print the temporary password: %w", printErr)
		}
		return nil
	}
	m, err := svc.FindMember(ctx, *email)
	if errors.Is(err, staffdomain.ErrMemberNotFound) {
		return fmt.Errorf("no staff member has the email %s", *email)
	}
	if err != nil {
		return fmt.Errorf("find staff member: %w", err)
	}
	switch cmd {
	case cmdDisable:
		_, err = svc.DisableMember(ctx, m.ID, by)
	case cmdEnable:
		_, err = svc.EnableMember(ctx, m.ID, by)
	case cmdUnlock:
		_, err = svc.UnlockMember(ctx, m.ID, by)
	case cmdResetPassword:
		var temp string
		if temp, err = svc.ResetMemberPassword(ctx, m.ID, by); err == nil {
			_, err = fmt.Fprintf(out, "temporary password: %s\n", temp)
		}
	default: // cmdResetTOTP
		_, err = svc.ResetMemberTOTP(ctx, m.ID, by)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", cmd, err)
	}
	if _, err = fmt.Fprintf(out, "%s %s: done\n", cmd, m.Email); err != nil {
		return fmt.Errorf("print the result: %w", err)
	}
	return nil
}
