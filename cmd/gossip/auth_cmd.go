package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/sipcapture/gossipper/internal/cli"
	"github.com/sipcapture/gossipper/internal/settingsauth"
)

func runAuthCommand(args []string) error {
	if len(args) == 0 || wantsSubcommandHelp(args) {
		fmt.Fprintln(os.Stderr, "usage: gossipper auth user-add -config <management.json> -username <name> -password <secret>")
		return errors.New("auth: missing subcommand")
	}
	switch args[0] {
	case "user-add":
		return runAuthUserAdd(args[1:])
	default:
		return fmt.Errorf("auth: unknown subcommand %q", args[0])
	}
}

func runAuthUserAdd(args []string) error {
	fs := flag.NewFlagSet("auth user-add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfgPath := fs.String("config", "", "flat management JSON (same as gossipper server -config)")
	username := fs.String("username", "", "login name")
	password := fs.String("password", "", "password (min 8 characters)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *cfgPath == "" || *username == "" || *password == "" {
		fs.Usage()
		return errors.New("auth user-add: -config, -username, and -password are required")
	}
	loaded, _, _, err := cli.LoadServerFlatOrComposite(*cfgPath)
	if err != nil {
		return err
	}
	if !loaded.AdminConsoleAuthEnabled() {
		return errors.New("config must set ui_data_dir or auth.type to internal")
	}
	path := loaded.AuthSQLitePath()
	if path == "" {
		return errors.New("config must set ui_data_dir or auth.sqlite_path")
	}
	auth, err := settingsauth.OpenBootstrap(path, loaded.Auth.JWTSecret)
	if err != nil {
		return err
	}
	defer auth.Close()
	if err := auth.CreateUser(context.Background(), *username, *password); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "user %q created or password updated in %s\n", *username, path)
	return nil
}
