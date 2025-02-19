package main

import (
	"fmt"
	"strings"
	"flag"
)

const (
	DefaultCharDevicePath  = "ja s pisnickou jdu jako ptacek"
	DefaultUnderDevicePath = "sladke mameni"
	DefaultIpAddress       = "la da da da da"
	DefaultPort            = 123456789
	DefaultFullScan        = false
	DefaultVerbose         = false
)

// Config holds data passed by arguments
type Config struct {
	CharDevicePath  string // character device to communicate with
	UnderDevicePath string // need to
	IpAddress       string // ip address of a server where to store backup
	Port            int    // port of the server
	Verbose         bool   // verbose output of the program
}

/* validate arguments that are passed in the program */
func ValidateArgs(charDevicePath *string, underDevicePath *string, ipAddress *string, port *int) error {
	var missingArgs []string

	// Nil checks to avoid dereferencing nil pointers
	if charDevicePath == nil || *charDevicePath == DefaultCharDevicePath {
		missingArgs = append(missingArgs, "-c (character device path)")
	}
	if underDevicePath == nil || *underDevicePath == DefaultUnderDevicePath {
		missingArgs = append(missingArgs, "-d ( device path)")
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

func (c *Config) VerbosePrintln(args ...interface{}) {
	if c.Verbose {
		fmt.Println(args...)
	}
}

func NewConfig() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.CharDevicePath, "c", DefaultCharDevicePath, "Path to character device we communicate with")
	flag.StringVar(&cfg.UnderDevicePath, "d", DefaultUnderDevicePath, "Path to underlying device, for full scan")
	flag.StringVar(&cfg.IpAddress, "I", DefaultIpAddress, "Receiver IP address")
	flag.IntVar(&cfg.Port, "p", DefaultPort, "Receiver port")
	flag.BoolVar(&cfg.Verbose, "v", DefaultVerbose, "Provides verbose output of the program")
	flag.Parse()
	return cfg
}
