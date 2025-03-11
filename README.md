# BDR: Block Device Replicator

## Introduction
BDR (Block Device Replicator) is a real-time, network-based block device replication solution designed for Linux. BDR observes writes made to a specified device and sends them over the network to a designated destination, where the receiver daemon writes them to a backup device—essentially mirroring RAID 1 functionality.

If the disk experiences high load and the client daemon cannot send new writes quickly enough, a complete disk scan is necessary. When the client daemon starts, it performs a full disk scan to ensure consistency. The initial scan can be turned off if you are sure that devices are identical (not advised).

## Features
- Real-time block-level replication
- Kernel module for efficient write tracking
- Userspace daemons for network communication
- Support for configurable buffer sizes to optimize performance
- Designed for Linux Kernel >= 6.8

## Requirements

* GNU Make
* Linux Kernel >= 6.8
* Linux Kernel Headers
* Go >= 1.23.0

## Installation

### Clone the Repository

First, clone the BDR repository to your local machine:

```bash
git clone https://github.com/JUaHELE/bdr.git
```

### Build the Kernel Driver

Navigate to the `kern/` directory and build the kernel driver, then load it into the kernel:

```bash
cd kern/
make
sudo insmod bdr.ko
```

### Build the Userspace Daemons

```bash
cd user/go/client
go build -o bdr_client

cd user/go/server
go build -o bdr_server
```

### Setup the Device Target

For this purpose, there is a `setup.sh` script in the `bdr` directory. If you want to manually create a device mapper target, the table has the structure:

```
underlying_device_path character_device_name buffer_size_in_writes
```

The buffer is a shared structure between the kernel and userspace daemon, where the kernel places writes. One write structure is 4104 bytes long. It's up to you how large the buffer should be, but it's advised to set it to more than 50MB to prevent complete scans as much as possible.

```bash
./setup.sh
```

## Usage

### Client Options

```
  -address string
    	Receiver IP address (required)
  -chardev string
    	Path to BDR character device (required)
  -debug
    	Provides debug output of the program
  -noprint
    	Disables prints
  -noreplication
    	Disables replication when started
  -port int
    	Receiver port
  -underdev string
    	Path to underlying device, used only for reading (required)
  -verbose
    	Provides verbose output of the program
```

### Server Options

```
  -address string
    	IP address to listen on (required)
  -debug
    	Provides debug output of the program
  -noprint
    	Disables prints
  -port int
    	Port to listen on
  -target string
    	Path to target device (required)
  -verbose
    	Provides verbose output of the program
```

## Removal

To remove the device mapper target:

```bash
dmsetup remove $target_name
```

To remove the kernel module:

```bash
rmmod bdr
```

## Configure Your Tunneling Solution (Recommended)
For secure and efficient replication over a network, it is recommended to use an encrypted tunneling solution such as SSH tunneling, WireGuard, or VPN. Ensure low-latency connectivity for optimal performance.

## Testing

The `test/` directory contains test scripts to verify correct functionality. Due to the nature of these tests, they must be executed with root privileges to set up test loop devices and load the kernel module.

```bash
sudo ./test/run_tests.sh
```
