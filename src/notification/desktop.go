// Package notification delivers JenkTui notifications outside the terminal.
package notification

import (
	"github.com/gen2brain/beeep"
)

const appName = "JenkTui"

// Options controls how a desktop notification is delivered.
type Options struct {
	Sound bool
	Alert bool
}

// Sender is the desktop-notification seam used by the app shell.
type Sender interface {
	Send(title, message string, options Options) error
}

type driver interface {
	Notify(title, message string) error
	Alert(title, message string) error
	Beep(freq float64, duration int) error
}

type beeepDriver struct{}

func (beeepDriver) Notify(title, message string) error {
	return beeep.Notify(title, message, "")
}

func (beeepDriver) Alert(title, message string) error {
	return beeep.Alert(title, message, "")
}

func (beeepDriver) Beep(freq float64, duration int) error {
	return beeep.Beep(freq, duration)
}

// Desktop sends native operating-system notifications through beeep.
type Desktop struct {
	driver driver
}

var _ Sender = (*Desktop)(nil)

// NewDesktop creates the production desktop notification sender.
func NewDesktop() *Desktop {
	beeep.AppName = appName
	return newDesktop(beeepDriver{})
}

func newDesktop(driver driver) *Desktop {
	return &Desktop{driver: driver}
}

// Send displays a desktop notification. Alerts are used only when both Alert
// and Sound are enabled because beeep.Alert always produces a sound. Ordinary
// notifications remain normal urgency and add sound through a separate beep.
func (d *Desktop) Send(title, message string, options Options) error {
	if options.Alert && options.Sound {
		return d.driver.Alert(title, message)
	}
	if err := d.driver.Notify(title, message); err != nil {
		return err
	}
	if options.Sound {
		return d.driver.Beep(beeep.DefaultFreq, beeep.DefaultDuration)
	}
	return nil
}
