package main

import (
	"strings"
	"testing"
)

func TestValidateArgs(t *testing.T) {
	tests := []struct {
		name            string
		charDevicePath  string
		underDevicePath string
		ipAddress       string
		port            int
		wantErr         bool
		errorContains   string
	}{
		{
			name:            "all valid arguments",
			charDevicePath:  "/dev/tty0",
			underDevicePath: "/dev/sda",
			ipAddress:       "192.168.1.1",
			port:            8080,
			wantErr:         false,
		},
		{
			name:            "missing char device path",
			charDevicePath:  DefaultCharDevicePath,
			underDevicePath: "/dev/sda",
			ipAddress:       "192.168.1.1",
			port:            8080,
			wantErr:         true,
			errorContains:   "character device path",
		},
		{
			name:            "missing all arguments",
			charDevicePath:  DefaultCharDevicePath,
			underDevicePath: DefaultUnderDevicePath,
			ipAddress:       DefaultIpAddress,
			port:            DefaultPort,
			wantErr:         true,
			errorContains:   "missing required arguments",
		},
		{
			name:            "nil arguments",
			charDevicePath:  "",
			underDevicePath: "",
			ipAddress:       "",
			port:            0,
			wantErr:         true,
			errorContains:   "missing required arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charPath := tt.charDevicePath
			underPath := tt.underDevicePath
			ip := tt.ipAddress
			port := tt.port

			err := ValidateArgs(&charPath, &underPath, &ip, &port)

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
