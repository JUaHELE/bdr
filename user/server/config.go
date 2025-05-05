// server
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const (
	DefaultPort             = 0
	DefaultIpAddress        = "required"
	DefaultTargetDevicePath = "required"
	DefaultJournalPath      = "required"
	DefaultVerbose          = false
	DefaultDebug            = false
	DefaultNoPrint          = false
	DefaultBenchmark        = false
)

type Config struct {
	Port          int
	IpAddress     string
	TargetDevPath string
	JournalPath   string
	Verbose       bool
	Debug         bool
	NoPrint       bool
	Benchmark     bool
}

func ValidateArgs(targetDevicePath *string, port *int, ipAddress *string, journalPath *string) error {
	var missingArgs []string

	if targetDevicePath == nil || *targetDevicePath == DefaultTargetDevicePath {
		missingArgs = append(missingArgs, "--target-device (target device path)")
	}
	if port == nil || *port == DefaultPort {
		missingArgs = append(missingArgs, "--port (port to listen on)")
	}
	if ipAddress == nil || *ipAddress == DefaultIpAddress {
		missingArgs = append(missingArgs, "--address (IP address to listen on)")
	}
	if journalPath == nil || *journalPath == DefaultJournalPath {
		missingArgs = append(missingArgs, "--journal (path to a device to store journal)")
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

	pflag.IntVar(&cfg.Port, "port", DefaultPort, "Port to listen on")
	pflag.StringVar(&cfg.IpAddress, "address", DefaultIpAddress, "IP address to listen on")
	pflag.StringVar(&cfg.TargetDevPath, "target-device", DefaultTargetDevicePath, "Path to target device")
	pflag.StringVar(&cfg.JournalPath, "journal", DefaultJournalPath, "Path to a device to store journal")
	pflag.BoolVar(&cfg.Verbose, "verbose", DefaultVerbose, "Provides verbose output of the program")
	pflag.BoolVar(&cfg.Debug, "debug", DefaultDebug, "Provides debug output of the program")
	pflag.BoolVar(&cfg.NoPrint, "no-print", DefaultNoPrint, "Disables prints")
	pflag.BoolVar(&cfg.Benchmark, "benchmark", DefaultBenchmark, "Enables benchmark")

	pflag.Parse()

	return cfg
}
