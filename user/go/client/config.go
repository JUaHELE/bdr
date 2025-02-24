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
	DefaultFullReplication    = false
	DefaultCheckedReplication = false
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
	FullReplication    bool   // replicates the whole disk without checking hashes of blocks
	CheckedReplication bool   // compares disks if they are the same by doing checksums of blocks
}

/* validate arguments that are passed in the program */
func ValidateArgs(charDevicePath *string, underDevicePath *string, ipAddress *string, port *int) error {
	var missingArgs []string

	// Nil checks to avoid dereferencing nil pointers
	if charDevicePath == nil || *charDevicePath == DefaultCharDevicePath {
		missingArgs = append(missingArgs, "-c (character device path)")
	}
	if underDevicePath == nil || *underDevicePath == DefaultUnderDevicePath {
		missingArgs = append(missingArgs, "-d (device path)")
	}
	if ipAddress == nil || *ipAddress == DefaultIpAddress {
		missingArgs = append(missingArgs, "-I (IP address)")
	}
	if port == nil || *port == DefaultPort {
		missingArgs = append(missingArgs, "-p (port)")
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

	fmt.Println(args...)
}

/* prints if verbose or debug prints are on */
func (c *Config) VerbosePrintln(args ...interface{}) {
	if c.NoPrint {
		return
	}

	if c.Verbose || c.Debug {
		fmt.Println(args...)
	}
}

/* prints olny if debug option is on */
func (c *Config) DebugPrintln(args ...interface{}) {
	if c.NoPrint {
		return
	}

	if c.Debug {
		fmt.Println(args...)
	}
}

func NewConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.CharDevicePath, "c", DefaultCharDevicePath, "Path to bdr character device")
	flag.StringVar(&cfg.UnderDevicePath, "d", DefaultUnderDevicePath, "Path to underlying device, used for only for reading")
	flag.StringVar(&cfg.IpAddress, "I", DefaultIpAddress, "Receiver IP address")
	flag.IntVar(&cfg.Port, "p", DefaultPort, "Receiver port")
	flag.BoolVar(&cfg.Verbose, "v", DefaultVerbose, "Provides verbose output of the program")
	flag.BoolVar(&cfg.Debug, "D", DefaultDebug, "Provides debug output of the program")
	flag.BoolVar(&cfg.FullReplication, "r", DefaultFullReplication, "Replicates whole device over the network")
	flag.BoolVar(&cfg.CheckedReplication, "R", DefaultCheckedReplication, "Initiates checked replication. Compares checksums of blocks on local and remote device, and those blocks are exchanged in case of missmatch")
	flag.BoolVar(&cfg.NoPrint, "n", DefaultNoPrint, "Disables prints")
	flag.Parse()
	return cfg
}
