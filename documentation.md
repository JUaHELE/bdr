#BDR: Block Device Replicator
# User Manual

## Table of Contents
1. [Introduction](#introduction)
   - [What is BDR?](#what-is-bdr)
2. [System Requirements](#system-requirements)
3. [Installation Guide](#installation-guide)
   - [Installing from Source](#installing-from-source)
   - [Building the Kernel Driver](#building-the-kernel-driver)
   - [Building the Userspace Daemons](#building-the-userspace-daemons)
4. [Configuration](#configuration)
   - [Setting Up the Device Target](#setting-up-the-device-target)
   - [Configuration Options](#configuration-options)
   - [Buffer Size Considerations](#buffer-size-considerations)
   - [Different Types of Recoveries](#different-types-of-recoveries)
5. [Usage](#usage)
   - [Client Configuration](#client-configuration)
   - [Server Configuration](#server-configuration)
   - [Starting Replication](#starting-replication)
6. [Security Considerations](#security-considerations)
   - [Network Security](#network-security)
   - [Setting Up SSH Tunneling](#setting-up-ssh-tunneling)
   - [Using WireGuard](#using-wireguard)
7. [Troubleshooting](#troubleshooting)
   - [Common Issues](#common-issues)
8. [Uninstallation](#uninstallation)
9. [Appendices](#appendices)
    - [Command Reference](#command-reference)
    - [Configuration File Reference](#configuration-file-reference)

## Introduction

### What is BDR?

BDR is a real-time, network-based block device replication solution for Linux systems. It's designed to provide data mirroring between storage devices across a network, similar to RAID 1 functionality but operating over network.

BDR works by monitoring writes made to a specified block device on a source system and replicating those writes to a destination device on a target system in real-time.

## System Requirements

Before installing BDR, ensure your system meets the following requirements:

- **Operating System**: Linux (kernel version 6.8 or higher)
- **Development Tools**:
  - GNU Make
  - Linux Kernel Headers
  - Go version 1.23.0 or higher
- **Storage**: Sufficient disk space on target system to mirror source device
- **Permissions**: Root access on both source and target systems

## Installation Guide

### Installing from Source

1. Clone the BDR repository from GitHub:

```bash
git clone https://github.com/JUaHELE/bdr.git
cd bdr
```

### Building the Kernel Driver

The kernel driver is responsible for intercepting and buffering write operations at the block device level.

```bash
cd kern/
make
sudo insmod bdr.ko
```

You can verify that the kernel module has loaded properly with:

```bash
lsmod | grep bdr
```

This should show the "bdr" module in the list of loaded kernel modules.

### Building the Userspace Daemons

BDR consists of two daemons:
- **Client daemon**: Runs on the source system and sends write operations to the server
- **Server daemon**: Runs on the target system and applies received write operations

To build these daemons:

```bash
# Build the client daemon
cd user/client
go build -o bdr_client

# Build the server daemon
cd user/server
go build -o bdr_server
```

For convenience, you can to copy these binaries to a directory in your system PATH, such as `/usr/local/bin/`:

```bash
sudo cp bdr_client /usr/local/bin/
sudo cp bdr_server /usr/local/bin/
```

## Configuration

### Setting Up the Device Target

BDR requires a device mapper target to intercept and buffer write operations. The provided `setup.sh` script can create this for you:

```bash
sudo ./setup.sh
```

This script will:
1. Create a character device for communication between the kernel module and userspace
2. Set up a device mapper target that redirects writes through the BDR module

If you prefer to set up the device target manually, you can use the following command:

```bash
echo "0 $(blockdev --getsz $underlying_device_path) bdr $underlying_device_path $character_device_name $buffer_size_in_writes" | sudo dmsetup create "$mapper_name"
```

Where:
- `$underlying_device_path` is the path to the block device you want to replicate (e.g., `/dev/sda1`)
- `$character_device_name` is the name for the character device BDR will create
- `$buffer_size_in_writes` is the number of write operations to buffer (one write structure is 4104 bytes long)
- `$mapper_name` is the name for the device mapper target which will be visible at `/dev/mapper/`

**Important**: After setup, you should only access the underlying device through the mapper path (`/dev/mapper/$mapper_name`), not directly through the original device path.

### Configuration Options

BDR provides several configuration options for both client and server daemons, which are detailed in the [Usage](#usage) section below.

### Buffer Size Considerations

The buffer size is a critical configuration parameter that affects both performance and reliability:

- **Buffer size**: Each write structure in the buffer requires 4104 bytes of memory
- **Total buffer memory**: Total memory used = buffer_size_in_writes × 4104 bytes
- **Selection factors**: Choose based on disk write load and network throughput

For typical usage:
- **Low-traffic disk**: 1000-5000 writes buffer (4-20 MB)
- **Medium-traffic disk**: 5000-20000 writes buffer (20-80 MB)
- **High-traffic disk**: 20000-100000 writes buffer (80-400 MB)

If the buffer fills up due to network congestion or high write rates, BDR will need to perform a scan to ensure consistency once the network connection is restored.

### Different Types of Recoveries

BDR provides several types of recovery mechanisms to maintain data consistency:

#### Journal Scan

This scan is initiated every time at the start of the client daemon unless specified `--no-replication` flag or when the buffer overflows. When this happens, some writes are lost to BDR and need to be recovered through scanning. The process works as follows:

1. The BDR server separates disk space into blocks and computes checksums
2. These checksums are sent to the client
3. The client compares them with the source disk checksums
4. If a block checksum mismatches between source and replica, the correct block is transmitted to the server
5. The correct block is written to the journal
6. After all correct blocks are transmitted, the buffer is sent and saved in the journal
7. Finally, the journal is written to disk

This approach ensures that the replica will stay in a prefix consistent state at all times, even during recovery operations.

#### After-Start Recoveries

When starting the BDR client daemon, you can choose between different recovery mechanisms:

1. **Full Scan**: Initiated when the client daemon is started with the `--full-scan` flag. This scan writes the correct blocks directly to the replica without using the journal. This method is intended for cases when journal space isn't sufficient to accommodate all correct blocks.

2. **Full Replication**: Initiated when the client daemon is started with the `--full-replication` flag. The BDR client daemon reads the entire disk and transmits it to the server. This feature is particularly useful when initializing a replica for the first time, as checksums will differ on every block.

**Note**: These approaches might temporarily corrupt the replica, so use with caution.

## Usage

### Client Configuration

The client daemon runs on the source system and is responsible for sending write operations to the server. It requires root privileges to access the block device.

**Basic client configuration**:

```bash
sudo bdr_client --control-device /dev/$character_device_name --source-device /dev/mapper/$mapper_name --address $server_ip --port $server_port
```

**Available client options**:

```
  --address string
        Receiver IP address (required)
  --full-scan
        Initiates direct full scan after start of the daemon
  --control-device string
        Path to BDR control character device (required)
  --no-print
        Disables standard output
  --no-replication
        Disables replication when started
  --port int
        Receiver port
  --source-device string
        Path to BDR source device mapper (required)
  --verbose
        Provides verbose output of the program
  --debug
        Provides debug output of the program
  --full-replication
        Transmits the entire disk at the start of the daemon
  --benchmark
        Enables benchmark info
```

**Example with full disk scan**:

```bash
sudo bdr_client --control-device /dev/bdr_char --source-device /dev/mapper/bdr_mapper --address 127.0.0.1 --port 8000 --verbose
```

This will:
1. Connect to the server at 127.0.0.1:8000
2. Perform a full disk scan to ensure initial consistency
3. Begin replicating writes with verbose logging enabled

### Server Configuration

The server daemon runs on the target system and applies received write operations to the target device. It also requires root privileges.

**Basic server configuration**:

```bash
sudo bdr_server --address $listen_ip --port $listen_port --target-device $target_device --journal $journal_path
```

**Available server options**:

```
  --address string
        IP address to listen on (required)
  --no-print
        Disables standard output
  --port int
        Port to listen on
  --target-device string
        Path to target device (required)
  --verbose
        Provides verbose output of the program
  --journal string
        Path to disk, where journal will be saved (required)
  --benchmark
        Enables benchmark
  --debug
        Provides debug output of the program
```

**Example server configuration**:

```bash
sudo bdr_server --address 127.0.0.1 --port 8000 --target-device /dev/sdb1 --journal /dev/loop0
```

This will:
1. Listen on loopback and on port 8000
2. Apply received write operations to /dev/sdb1
3. Use /dev/loop0 for the write journal

### Starting Replication

1. **Start the server daemon first** on the target system
2. **Start the client daemon** on the source system

Replication begins automatically once both daemons are running, unless the `--no-replication` flag is used on the client.

## Security Considerations

BDR transfers raw block-level data over the network. By default, this data is not encrypted, which could pose security risks if the traffic traverses untrusted networks.

### Network Security

For secure operation, consider the following:

1. **Use an isolated network** for replication traffic if possible
2. **Implement firewall rules** to restrict access to the BDR ports
3. **Use encrypted tunneling** for traffic traversing untrusted networks

### Setting Up SSH Tunneling

SSH tunneling provides a simple way to encrypt BDR traffic:

On the source system:

```bash
ssh -L 8000:localhost:8000 user@target_system -N &
```

Then configure the client to connect to localhost:

```bash
sudo bdr_client --control-device /dev/bdr_char --source-device /dev/mapper/bdr_mapper --address localhost --port 8000
```

### Using WireGuard

WireGuard provides an efficient VPN solution for BDR traffic:

1. **Install WireGuard** on both source and target systems
2. **Configure WireGuard** to establish a secure tunnel
3. **Configure BDR** to use the WireGuard interface addresses

## Troubleshooting

### Common Issues

**Client cannot connect to server**:
- Verify network connectivity (ping, traceroute)
- Check firewall settings on both systems
- Ensure server daemon is running and listening on the correct interface/port

**Replication falling behind**:
- Increase buffer size
- Check network bandwidth and latency
- Reduce write load on the source system if possible

**Kernel module fails to load**:
- Verify kernel version (6.8 or higher required)
- Check kernel logs: `dmesg | grep bdr`

**Full disk scan taking too long**:
- This is normal for large devices, slow disks, or slow network connections
- For very large devices, consider seeding the target device before starting BDR

## Uninstallation

To remove BDR from your system:

1. **Stop the client and server daemons**:
   ```bash
   killall bdr_client
   killall bdr_server
   ```

2. **Remove the device mapper target**:
   ```bash
   sudo dmsetup remove $mapper_name
   ```

3. **Unload the kernel module**:
   ```bash
   sudo rmmod bdr
   ```

4. **Remove the binaries** if they were copied to system directories:
   ```bash
   sudo rm /usr/local/bin/bdr_client
   sudo rm /usr/local/bin/bdr_server
   ```

## Appendices

### Command Reference

**Kernel Module Management**:
```bash
# Load kernel module
sudo insmod bdr.ko

# Unload kernel module
sudo rmmod bdr

# Check if module is loaded
lsmod | grep bdr
```

**Device Mapper Management**:
```bash
# Create device mapper target
echo "0 $(blockdev --getsz $underlying_device_path) bdr $underlying_device_path $character_device_name $buffer_size_in_writes" | sudo dmsetup create "$mapper_name"

# Remove device mapper target
sudo dmsetup remove $mapper_name

# Check device mapper status
sudo dmsetup status $mapper_name
sudo dmsetup table $mapper_name
```

**Client Daemon**:
```bash
sudo bdr_client --control-device /dev/$character_device_name --source-device /dev/mapper/$mapper_name --address $server_ip --port $server_port [options]
```

**Server Daemon**:
```bash
sudo bdr_server --address $listen_ip --port $listen_port --target-device $target_device --journal $journal_path [options]
```

### Configuration File Reference

BDR does not currently use configuration files, relying instead on command-line parameters. However, you can create shell scripts to encapsulate your preferred configuration:

**Example client script** (`start_bdr_client.sh`):
```bash
#!/bin/bash
# BDR Client Configuration
CHAR_DEV="/dev/bdr_char"
MAPPER_DEV="/dev/mapper/bdr_mapper"
SERVER_IP="127.0.0.1"
SERVER_PORT=8000
BUFFER_SIZE=10000  # Used in setup

# Setup device mapper if not already set up
if ! dmsetup info $MAPPER_DEV >/dev/null 2>&1; then
    echo "Setting up device mapper..."
    echo "0 $(blockdev --getsz /dev/sda1) bdr /dev/sda1 bdr_char $BUFFER_SIZE" | sudo dmsetup create bdr_mapper
fi

# Start BDR client
sudo bdr_client --control-device $CHAR_DEV --source-device $MAPPER_DEV --address $SERVER_IP --port $SERVER_PORT --verbose
```

**Example server script** (`start_bdr_server.sh`):
```bash
#!/bin/bash
# BDR Server Configuration
LISTEN_IP="127.0.0.1"
LISTEN_PORT=8000
TARGET_DEV="/dev/sdb1"
JOURNAL_PATH="/dev/loop0"

# Start BDR server
sudo bdr_server --address $LISTEN_IP --port $LISTEN_PORT --target-device $TARGET_DEV --journal $JOURNAL_PATH --verbose
```

Make these scripts executable and run them to start BDR with your preferred configuration:
```bash
chmod +x start_bdr_client.sh
chmod +x start_bdr_server.sh
```
