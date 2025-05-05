package main

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

const (
	DefaultCharDevicePath     = "required"
	DefaultUnderDevicePath    = "required"
	DefaultIpAddress          = "required"
	DefaultPort               = 0
	DefaultVerbose            = false
	DefaultDebug              = false
	DefaultNoPrint            = false
	DefaultInitialReplication = false
	DefaultBenchmark          = false
	DefaultFullScan           = false
	DefaultFullReplicate      = false
)

// Config holds data passed by arguments
type Config struct {
	CharDevicePath     string
	UnderDevicePath    string
	IpAddress          string
	Port               int
	Verbose            bool
	Debug              bool
	NoPrint            bool
	InitialReplication bool
	FullReplicate      bool
	FullScan           bool
	Benchmark          bool
}

func ValidateArgs(charDevicePath *string, underDevicePath *string, ipAddress *string, port *int) error {
	var missingArgs []string

	if charDevicePath == nil || *charDevicePath == DefaultCharDevicePath {
		missingArgs = append(missingArgs, "--control-device (character device path)")
	}
	if underDevicePath == nil || *underDevicePath == DefaultUnderDevicePath {
		missingArgs = append(missingArgs, "--source-device (underlying device path)")
	}
	if ipAddress == nil || *ipAddress == DefaultIpAddress {
		missingArgs = append(missingArgs, "--address (IP address)")
	}
	if port == nil || *port == DefaultPort {
		missingArgs = append(missingArgs, "--port (port)")
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

func (c *Config) VerbosePrintln(args ...interface{}) {
	if c.NoPrint {
		return
	}
	if c.Verbose || c.Debug {
		fmt.Println("[VERBOSE]:", args)
	}
}

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

	// Define flags with better problem-domain names
	pflag.StringVar(&cfg.CharDevicePath, "control-device", DefaultCharDevicePath, "Path to BDR control character device")
	pflag.StringVar(&cfg.UnderDevicePath, "source-device", DefaultUnderDevicePath, "Path to BDR source device mapper")
	pflag.StringVar(&cfg.IpAddress, "address", DefaultIpAddress, "Receiver IP address")
	pflag.IntVar(&cfg.Port, "port", DefaultPort, "Receiver port")
	pflag.BoolVar(&cfg.Verbose, "verbose", DefaultVerbose, "Provides verbose output of the program")
	pflag.BoolVar(&cfg.Debug, "debug", DefaultDebug, "Provides debug output of the program")
	pflag.BoolVar(&cfg.NoPrint, "no-print", DefaultNoPrint, "Disables standard output")
	pflag.BoolVar(&cfg.InitialReplication, "no-replication", DefaultInitialReplication, "Disables replication when started")
	pflag.BoolVar(&cfg.Benchmark, "benchmark", DefaultBenchmark, "Enables benchmark info")
	pflag.BoolVar(&cfg.FullScan, "full-scan", DefaultFullScan, "Performs full scan at the start of the daemon")
	pflag.BoolVar(&cfg.FullReplicate, "full-replication", DefaultFullReplicate, "Transmits the entire disk at the start of the daemon")

	// Parse flags
	pflag.Parse()

	cfg.InitialReplication = !cfg.InitialReplication

	return cfg
}
