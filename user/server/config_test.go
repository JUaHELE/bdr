package main

import (
	"strings"
	"testing"
)

func TestValidateArgs(t *testing.T) {
	tests := []struct {
		name             string
		targetDevicePath string
		port             int
		ipAddress        string
		journalPath string
		wantErr          bool
		errorContains    string
	}{
		{
			name:             "all valid arguments",
			targetDevicePath: "/dev/sda",
			ipAddress:        "192.168.1.1",
			port:             8080,
			journalPath: "/dev/sda",
			wantErr:          false,
		},
		{
			name:             "missing target device path",
			targetDevicePath: DefaultTargetDevicePath,
			ipAddress:        "192.168.1.1",
			port:             8080,
			journalPath: "/dev/sda",
			wantErr:          true,
			errorContains:    "target device path",
		},
		{
			name:             "missing port",
			targetDevicePath: "/dev/sda",
			ipAddress:        "192.168.1.1",
			port:             DefaultPort,
			journalPath: "/dev/sda",
			wantErr:          true,
			errorContains:    "port",
		},
		{
			name:             "missing IP address",
			targetDevicePath: "/dev/sda",
			ipAddress:        DefaultIpAddress,
			port:             8080,
			journalPath: "/dev/sda",
			wantErr:          true,
			errorContains:    "IP address",
		},
		{
			name:             "missing all arguments",
			targetDevicePath: DefaultTargetDevicePath,
			ipAddress:        DefaultIpAddress,
			port:             DefaultPort,
			journalPath: "/dev/sda",
			wantErr:          true,
			errorContains:    "missing required arguments",
		},
		{
			name:             "nil arguments test",
			targetDevicePath: "",
			ipAddress:        "",
			port:             0,
			journalPath: "/dev/sda",
			wantErr:          true,
			errorContains:    "missing required arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetPath := tt.targetDevicePath
			port := tt.port
			ip := tt.ipAddress
			journalPath := tt.journalPath

			err := ValidateArgs(&targetPath, &port, &ip, &journalPath)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateArgs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && !strings.Contains(err.Error(), tt.errorContains) {
				t.Errorf("ValidateArgs() error = %v, should contain %v", err, tt.errorContains)
			}
		})
	}
}
