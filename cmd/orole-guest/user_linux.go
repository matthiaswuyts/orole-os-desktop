//go:build linux

package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/guestagent"
)

// botUID is the guest image's unprivileged account (guest/configuration.nix). Its name and home follow the bot's
// name (orole-identity renames it at boot, before this agent starts), so it is looked up by uid, never by name.
const botUID = "1000"

// guestUser makes commands and terminals run as the bot's account, through login shells so they get the system
// environment (PATH, nix-ld) from /etc/profile rather than the service's minimal one. Relative paths resolve against
// the account's current home from passwd (/home/<name>), and HOME, USER and LOGNAME come from the same entry.
func guestUser(cfg *guestagent.Config) error {
	id, home, err := lookupIdentityByUID(botUID)
	if err != nil {
		return err
	}
	cfg.RunAs, cfg.Home = id, home
	cfg.Shell = []string{"/bin/sh", "-lc"}
	cfg.LoginShell = []string{"/bin/sh", "-l"}
	if _, err := os.Stat(nixosBash); err == nil {
		cfg.LoginShell = []string{nixosBash, "-l"}
	}
	return nil
}

// nixosBash is bash on the NixOS guest image; other guests (the Alpine real-boot test) get /bin/sh.
const nixosBash = "/run/current-system/sw/bin/bash"

func lookupIdentityByUID(uid string) (*guestagent.Identity, string, error) {
	u, err := user.LookupId(uid)
	if err != nil {
		return nil, "", fmt.Errorf("guest user uid %s: %w", uid, err)
	}
	return identityOf(u)
}

func lookupIdentity(name string) (*guestagent.Identity, string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return nil, "", fmt.Errorf("guest user %s: %w", name, err)
	}
	return identityOf(u)
}

func identityOf(u *user.User) (*guestagent.Identity, string, error) {
	name := u.Username
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, "", fmt.Errorf("guest user %s: uid %q: %w", name, u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, "", fmt.Errorf("guest user %s: gid %q: %w", name, u.Gid, err)
	}
	id := &guestagent.Identity{Name: u.Username, UID: uint32(uid), GID: uint32(gid)}
	gids, err := u.GroupIds()
	if err != nil {
		return nil, "", fmt.Errorf("guest user %s: groups: %w", name, err)
	}
	for _, g := range gids {
		n, err := strconv.ParseUint(g, 10, 32)
		if err != nil {
			return nil, "", fmt.Errorf("guest user %s: group %q: %w", name, g, err)
		}
		id.Groups = append(id.Groups, uint32(n))
	}
	return id, u.HomeDir, nil
}
