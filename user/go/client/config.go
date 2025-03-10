// client

package main

import (
	"flag"
	"fmt"
	"strings"
)

const (
	DefaultCharDevicePath     = "required"
	DefaultUnderDevicePath    = "required"
	DefaultIpAddress          = "required"
	DefaultPort               = 0
	DefaultFullScan           = false
	DefaultVerbose            = false
	DefaultDebug              = false
	DefaultNoPrint            = false
	DefaultInitialReplication = false
)

// Config holds data passed by arguments
type Config struct {
	CharDevicePath     string // character device to communicate with
	UnderDevicePath    string // need to
	IpAddress          string // ip address of a server where to store backup
	Port               int    // port of the server
	Verbose            bool   // verbose output of the program
	Debug              bool   // includes debug prints to verbose
	NoPrint            bool   // to prints except error messages
	InitialReplication bool   // compares disks if they are the same by doing checksums of blocks
}

/* validate arguments that are passed in the program */
func ValidateArgs(charDevicePath *string, underDevicePath *string, ipAddress *string, port *int) error {
	var missingArgs []string

	// Nil checks to avoid dereferencing nil pointers
	if charDevicePath == nil || *charDevicePath == DefaultCharDevicePath {
		missingArgs = append(missingArgs, "-chardev (character device path)")
	}
	if underDevicePath == nil || *underDevicePath == DefaultUnderDevicePath {
		missingArgs = append(missingArgs, "-underdev (undelying device path)")
	}
	if ipAddress == nil || *ipAddress == DefaultIpAddress {
		missingArgs = append(missingArgs, "-address (IP address)")
	}
	if port == nil || *port == DefaultPort {
		missingArgs = append(missingArgs, "-port (port)")
	}

	// If there are missing arguments, return an error
	if len(missingArgs) > 0 {
		return fmt.Errorf("missing required arguments: %s", strings.Join(missingArgs, ", "))
	}

	return nil
}

func (c *Config) Println(args ...interface{}) {
	if c.NoPrint {
		return
	}

	fmt.Println("[INFO]:", args)
}

/* prints if verbose or debug prints are on */
func (c *Config) VerbosePrintln(args ...interface{}) {
	if c.NoPrint {
		return
	}

	if c.Verbose || c.Debug {
		fmt.Println("[VERBOSE]:", args)
	}
}

/* prints olny if debug option is on */
func (c *Config) DebugPrintln(args ...interface{}) {
	if c.NoPrint {
		return
	}

	if c.Debug {
		fmt.Println("[DEBUG]:", args)
	}
}

func NewConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.CharDevicePath, "chardev", DefaultCharDevicePath, "Path to bdr character device")
	flag.StringVar(&cfg.UnderDevicePath, "underdev", DefaultUnderDevicePath, "Path to underlying device, used for only for reading")
	flag.StringVar(&cfg.IpAddress, "address", DefaultIpAddress, "Receiver IP address")
	flag.IntVar(&cfg.Port, "port", DefaultPort, "Receiver port")
	flag.BoolVar(&cfg.Verbose, "verbose", DefaultVerbose, "Provides verbose output of the program")
	flag.BoolVar(&cfg.Debug, "debug", DefaultDebug, "Provides debug output of the program")
	flag.BoolVar(&cfg.NoPrint, "noprint", DefaultNoPrint, "Disables prints")
	flag.BoolVar(&cfg.InitialReplication, "noreplication", DefaultInitialReplication, "Disables replication when started")
	flag.Parse()

	cfg.InitialReplication = !cfg.InitialReplication

	return cfg
}
