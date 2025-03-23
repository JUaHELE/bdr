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
		wantErr          bool
		errorContains    string
	}{
		{
			name:             "all valid arguments",
			targetDevicePath: "/dev/sda",
			ipAddress:        "192.168.1.1",
			port:             8080,
			wantErr:          false,
		},
		{
			name:             "missing target device path",
			targetDevicePath: DefaultTargetDevicePath,
			ipAddress:        "192.168.1.1",
			port:             8080,
			wantErr:          true,
			errorContains:    "target device path",
		},
		{
			name:             "missing port",
			targetDevicePath: "/dev/sda",
			ipAddress:        "192.168.1.1",
			port:             DefaultPort,
			wantErr:          true,
			errorContains:    "port",
		},
		{
			name:             "missing IP address",
			targetDevicePath: "/dev/sda",
			ipAddress:        DefaultIpAddress,
			port:             8080,
			wantErr:          true,
			errorContains:    "IP address",
		},
		{
			name:             "missing all arguments",
			targetDevicePath: DefaultTargetDevicePath,
			ipAddress:        DefaultIpAddress,
			port:             DefaultPort,
			wantErr:          true,
			errorContains:    "missing required arguments",
		},
		{
			name:             "nil arguments test",
			targetDevicePath: "",
			ipAddress:        "",
			port:             0,
			wantErr:          true,
			errorContains:    "missing required arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetPath := tt.targetDevicePath
			port := tt.port
			ip := tt.ipAddress

			err := ValidateArgs(&targetPath, &port, &ip)

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
