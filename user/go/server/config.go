// server

package main

import (
	"flag"
	"fmt"
	"strings"
)

const (
	DefaultPort             = 0
	DefaultIpAddress        = "required"
	DefaultTargetDevicePath = "required"
	DefaultVerbose          = false
	DefaultDebug            = false
	DefaultNoPrint          = false
)

type Config struct {
	Port          int
	IpAddress     string
	TargetDevPath string
	Verbose       bool
	Debug         bool
	NoPrint       bool
}

func ValidateArgs(targetDevicePath *string, port *int, ipAddress *string) error {
	var missingArgs []string

	if targetDevicePath == nil || *targetDevicePath == DefaultTargetDevicePath {
		missingArgs = append(missingArgs, "-target (target device path)")
	}

	if port == nil || *port == DefaultPort {
		missingArgs = append(missingArgs, "-port (port to listen on)")
	}

	if ipAddress == nil || *ipAddress == DefaultIpAddress {
		missingArgs = append(missingArgs, "-address (IP address to listen on)")
	}

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

	flag.IntVar(&cfg.Port, "port", DefaultPort, "Port to listen on")
	flag.StringVar(&cfg.IpAddress, "address", DefaultIpAddress, "IP address to listen on")
	flag.StringVar(&cfg.TargetDevPath, "target", DefaultTargetDevicePath, "Path to target device")
	flag.BoolVar(&cfg.Verbose, "verbose", DefaultVerbose, "Provides verbose output of the program")
	flag.BoolVar(&cfg.Debug, "debug", DefaultDebug, "Provides debug output of the program")
	flag.BoolVar(&cfg.NoPrint, "noprint", DefaultNoPrint, "Disables prints")
	flag.Parse()
	return cfg
}
