package links2

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"time"
	"unicode/utf8"

	"github.com/Netflix/go-expect"
)

const (
	ctrlC = "\003" // ^C
	esc   = "\033" // Esc
)

const (
	dropdownMenu = "File  \033[0;7m  View    Link    Downloads    Setup    Help"
	exitLinks    = "Exit Links \033[0;7m-------------+"
	exitPrompt   = "Do you really want to exit Links?"
	goToMenu     = "Go to URL \033[0;7m---------------------------+"
)

const (
	lookupHost        = "Looking up host\033[0m"
	makeConnection    = "Making connection\033[0m"
	requestSent       = "Request sent\033[0m"
	sslNegotiate      = "SSL negotiation\033[0m"
	formatDocument    = "Formatting document\033[0m"
	errorLoading      = "Error loading "
	hostNotFound      = "Host not found"
	errorText         = "Error \033[0;7m"
	noSuchFile        = "No such file or directory\033[13;"
	fileAlreadyExists = "File already exists \033[10;"
)

type state int

const (
	stateUndefined state = iota // stateUndefined initial state of the Browser, or after calling Close.
	stateStarted                // stateStarted a call was made to Open or OpenContext; we might see a welcome screen.
	stateIdle                   // stateIdle no other menus are open (we can run most commands).
	stateMenu                   // stateMenu a menu is open.
)

const (
	menuDropdown = "dropdown"
	menuSearch   = "search"
	menuRSearch  = "rsearch"
	menuGoTo     = "goto"
)

// Browser represents an instance of a links2 process attached to an `expect`-like console controller.
type Browser struct {
	cmd        *exec.Cmd
	s          state
	c          *expect.Console
	menuName   string
	viewSource bool
}

// Open the browser subprocess.
func (b *Browser) Open() error { return b.OpenContext(context.Background()) }

// Open the browser subprocess passing in the given context.
func (b *Browser) OpenContext(ctx context.Context) error {
	switch b.s {
	case stateUndefined:
	default:
		return fmt.Errorf("browser already started")
	}

	cmd := exec.CommandContext(ctx, "links2")
	c, err := expect.NewConsole(expect.WithLogger(log.Default()))
	if err != nil {
		return err
	}
	cmd.Stdin = c.Tty()
	cmd.Stdout = c.Tty()
	cmd.Stderr = c.Tty()

	if err := cmd.Start(); err != nil {
		return err
	}

	b.cmd = cmd
	b.c = c
	b.s = stateStarted
	return nil
}

// Close stops the browser subprocess and resets it.
func (b *Browser) Close() error {
	err := b.c.Close()
	err1 := b.cmd.Cancel()
	if err == nil {
		err = err1
	}
	*b = Browser{}
	return err
}

func (b *Browser) Wait() (err error) {
	defer func() {
		if err1 := b.Close(); err == nil {
			err = err1
		}
	}()
	return b.cmd.Wait()
}

func (b *Browser) expectWelcomeScreen() bool {
	switch b.s {
	case stateStarted:
	default:
		return false
	}
	_, err := b.c.Expect(
		expect.String("Welcome"),
		expect.String("Welcome to links!"),
		expect.WithTimeout(0),
	)
	return err == nil
}

func (b *Browser) sendIdle(s string) error {
	if err := b.closeMenu(); err != nil {
		return err
	}
	_, err := b.c.Send(s)
	return err
}

func (b *Browser) expectGoToMenu() { b.c.ExpectString(goToMenu) }

func (b *Browser) expectDropDownMenu() {
	switch b.s {
	case stateMenu:
		return
	}
	b.c.ExpectString(dropdownMenu)
	b.s = stateMenu
}

func (b *Browser) openDropDownMenu() error {
	switch b.s {
	case stateMenu:
		return nil
	}
	if err := b.closeMenu(); err != nil {
		return err
	}
	b.c.Send("\033") // Esc
	b.expectDropDownMenu()
	return nil
}

func (b *Browser) closeMenu() error {
	switch b.s {
	case stateUndefined:
		return fmt.Errorf("browser not started")
	case stateStarted:
		if !b.expectWelcomeScreen() {
			b.s = stateIdle
			return nil
		}
	case stateIdle:
		return nil
	case stateMenu:
	}
	b.c.Send("\033") // Esc
	b.s = stateIdle
	b.menuName = ""
	return nil
}

// Navigate the browser to the given URL.
func (b *Browser) Navigate(rawURL string) error {
	// This serves to sanitize URL to ensure it has no terminal commands within.
	if !utf8.ValidString(rawURL) {
		return fmt.Errorf("url is not a valid unicode string: %q", rawURL)
	}
	// Parse the URL and possibly fix the scheme.
	// Links2 sometimes adds a scheme which can be weird.
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if u.Host == "" {
		u.Scheme = "file"
	}
	// Open GoTo menu.
	if err := b.sendIdle("g"); err != nil {
		return err
	}
	b.expectGoToMenu()

	// Hack? Ending with Esc (menu) and calling expectMenu is
	// the easiest way to determine when the page load finishes.
	fmt.Fprint(b.c, u.String(), "\n\033")
	b.expectDropDownMenu()
	return nil
}

func (b *Browser) ViewSource() {
	if !b.viewSource {
		b.c.Send("\\")
		b.viewSource = true
	}
}

func (b *Browser) ViewHTML() {
	if b.viewSource {
		b.c.Send("\\")
		b.viewSource = false
	}
}

// SaveFormattedDocument.
func (b *Browser) SaveFormattedDocument(name string, overwrite bool) {
	b.openDropDownMenu()
	b.c.Send("\033fd") // Alt-F d
	fmt.Fprint(b.c, "\033fd", name, "\n")
	// Handle "file already exists".
	if _, err := b.c.Expect(
		expect.String(fileAlreadyExists),
		expect.WithTimeout(10*time.Microsecond),
	); err == nil {
		if overwrite {
			b.c.Send("\n")
		} else {
			b.c.Send("\033") // Esc
		}
	}
}

// Quit the browser gracefully and return the error if any.
func (b *Browser) Quit() (err error) {
	if err := b.closeMenu(); err != nil {
		return err
	}
	defer func() {
		if err1 := b.Close(); err == nil {
			err = err1
		}
	}()
	_, err = b.c.Send("\003") // ^C
	return err
}

func (b *Browser) ScrollUp()   { b.sendIdle("\033[5~") }
func (b *Browser) ScrollDown() { b.sendIdle("\033[6~") }

func (b *Browser) ScrollLeft()  { b.sendIdle("[") }
func (b *Browser) ScrollRight() { b.sendIdle("]") }

// TODO: Provide a means to get the text and URL of the current link.

func (b *Browser) SelectNextLink() { b.sendIdle("\033[B") }
func (b *Browser) SelectPrevLink() { b.sendIdle("\033[A") }
func (b *Browser) FollowLink()     { b.sendIdle("\033[C") }
func (b *Browser) BackLink()       { b.sendIdle("\033[D") }

func (b *Browser) Reload()   { b.sendIdle("\022\033"); b.expectDropDownMenu() }
func (b *Browser) JumpEnd()  { b.sendIdle("\033[F") }
func (b *Browser) JumpHome() { b.sendIdle("\033[H") }

// TODO: Handle Search string not found and allow extracting and clearing results.

func (b *Browser) Search()         { b.sendIdle("/") }
func (b *Browser) SearchBackward() { b.sendIdle("?") }
func (b *Browser) FindNext()       { b.sendIdle("n") }
func (b *Browser) FindPrevious()   { b.sendIdle("N") }

type DocumentInfo struct{}

// FIXME: Allow extracting document info.
func (b *Browser) DocumentInfo() DocumentInfo {
	defer b.closeMenu()
	b.sendIdle("=")
	// TODO: Extract info.
	return DocumentInfo{}
}

type HTTPHeader struct{}

// FIXME: Allow extracting HTTP header.
func (b *Browser) HTTPHeader() HTTPHeader {
	defer b.closeMenu()
	b.sendIdle("|")
	// TODO: Extract info
	return HTTPHeader{}
}
