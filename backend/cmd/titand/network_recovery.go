package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"golang.org/x/sys/unix"
)

func networkRecovery(dir string) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("local recovery requires root and an interactive console")
	}
	fd := int(os.Stdin.Fd())
	term, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return fmt.Errorf("interactive terminal required: %w", err)
	}
	r := bufio.NewReader(os.Stdin)
	read := func(prompt string) (string, error) {
		fmt.Print(prompt)
		s, e := r.ReadString('\n')
		return strings.TrimSpace(s), e
	}
	fmt.Println("TitanOS · Netzwerk wiederherstellen / Network recovery")
	name, err := read("NAS-Administratorkonto / NAS administrator: ")
	if err != nil {
		return err
	}
	private := *term
	private.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &private); err != nil {
		return err
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, term)
	password, readErr := read("Passwort / Password: ")
	unix.IoctlSetTermios(fd, unix.TCSETS, term)
	fmt.Println()
	if readErr != nil {
		return readErr
	}
	store := config.Open(dir)
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		return err
	}
	user, ok := users.Authenticate(strings.ToLower(name), password)
	password = ""
	if !ok || !user.Role.CanAdminister() {
		time.Sleep(3 * time.Second)
		return fmt.Errorf("authentication failed")
	}
	if user.TwoFactor() {
		code, e := read("2FA-Code / recovery code: ")
		if e != nil || !users.CheckSecondFactor(user.Name, code) {
			return fmt.Errorf("second factor failed")
		}
	}
	m := network.NewManager(store)
	fmt.Println("1: Letzte bestätigte Konfiguration / Previous confirmed configuration\n2: DHCP auf einem Anschluss / DHCP on one port\n3: SSH wieder aus allen Netzen erlauben / Allow SSH from all networks again\n0: Abbrechen / Cancel")
	choice, err := read("Auswahl / Choice: ")
	if err != nil {
		return err
	}
	port := ""
	switch choice {
	case "0":
		return nil
	case "3":
		return clearSSHNetworks(store, read)
	case "1":
	case "2":
		for _, p := range m.Interfaces() {
			fmt.Printf("  %s (link=%t)\n", p.Name, p.Carrier)
		}
		port, err = read("Anschluss / Port: ")
		if err != nil || port == "" {
			return fmt.Errorf("no port selected")
		}
	default:
		return fmt.Errorf("invalid choice")
	}
	confirm, err := read("Netzwerk ändern? Tippe JA / Type YES to change networking: ")
	if err != nil || !(confirm == "JA" || confirm == "YES") {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "systemctl", "stop", "titand.service").CombinedOutput(); err != nil {
		return fmt.Errorf("stop daemon: %w: %s", err, out)
	}
	defer func() {
		// A context of its own: the one above may have run out meanwhile.
		start, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(start, "systemctl", "start", "titand.service").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "start daemon: %v: %s\n", err, out)
		}
	}()
	if err := m.Recover(ctx, port); err != nil {
		return err
	}
	fmt.Println("Konfiguration wiederhergestellt / Configuration restored")
	return nil
}

// clearSSHNetworks lifts the list of networks SSH logins may come from.
// titand is stopped meanwhile: it keeps the settings in memory and would
// otherwise write the old list back with its next change.
func clearSSHNetworks(store *config.Store, read func(string) (string, error)) error {
	confirm, err := read("SSH-Netzbeschränkung aufheben? Tippe JA / Type YES to allow SSH from all networks: ")
	if err != nil || !(confirm == "JA" || confirm == "YES") {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "systemctl", "stop", "titand.service").CombinedOutput(); err != nil {
		return fmt.Errorf("stop daemon: %w: %s", err, out)
	}
	defer func() {
		// A context of its own: the one above may have run out meanwhile.
		start, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(start, "systemctl", "start", "titand.service").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "start daemon: %v: %s\n", err, out)
		}
	}()
	changed, err := sshd.ClearNetworks(store)
	if err != nil {
		return err
	}
	if changed {
		fmt.Println("SSH ist wieder aus allen Netzen erreichbar / SSH allowed from all networks again")
	} else {
		fmt.Println("SSH war nicht auf Netze beschränkt / SSH was not limited to networks")
	}
	return nil
}
