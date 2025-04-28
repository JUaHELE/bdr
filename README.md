# BDR: Block Device Replicator

## Introduction
BDR (Block Device Replicator) is a real-time, network-based block device replication solution designed for Linux. BDR observes writes made to a specified device and sends them over the network to a designated destination, where the receiver daemon writes them to a backup device, essentially mirroring RAID 1 functionality.

If the disk experiences high load and the client daemon cannot send new writes quickly enough, a complete disk scan is necessary. When the client daemon starts, it performs a full disk scan to ensure consistency. The initial scan can be turned off if you are sure that devices are identical (not advised).

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

For this purpose, there is a `setup.sh` script in the `bdr` directory. If you want to manually create a device mapper target, you can do that with following command:

```bash
echo "0 $(blockdev --getsz $underlying_device_path) bdr $underlying_device_path $character_device_name $buffer_size_in_writes" | sudo dmsetup create "$mapper_name"
```

After successfull target setup you should see a character device at `/dev/$character_device_name` and device mapper at `/dev/$mapper_name`.

You should now only communicate with the underlying device through the the mapper path, not the original `$underlying_device_path`

The buffer is a shared structure between the kernel and userspace daemon, where the kernel places write information. One write structure is 4104 bytes long. The size of the buffer depends on load of the dis k and the speed of the internet connection.

```bash
sudo ./setup.sh
```

The `setup.sh` script has to run with root privileges, because of dmsetup

## Usage

### Client Options

```
  -address string
    	Receiver IP address (required)
  -fullscan
    	Initiates direct full scan after start of the daemon
  -chardev string
    	Path to BDR character device (required)
  -noprint
    	Disables prints
  -noreplication
    	Disables replication when started
  -port int
    	Receiver port
  -mapperdev string
    	Path to underlying device, used only for reading (required)
  -verbose
    	Provides verbose output of the program
```

To clarify `-chardev` path specifies character device created in target setup step (`/dev/$character_device_name`) and the `-mapperdev` specifies the mapper created in target setup step (`/dev/$mapper_name`).

Client has to run with root privileges since it has to open character device and the mapper device. The mapper device is used only for reading the disk when full scanning

### Server Options

```
  -address string
    	IP address to listen on (required)
  -noprint
    	Disables prints
  -port int
    	Port to listen on
  -target string
    	Path to target device (required)
  -verbose
    	Provides verbose output of the program
  -journal string
    	Path to disk, where journal will be saved (required)
```

Server has to also run with root privileges because it has to open the target device specified by `-target` to propagate reads.

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
For secure replication over a network, it is recommended to use an encrypted tunneling solution such as SSH tunneling, WireGuard, or VPN. Ensure low-latency connectivity for optimal performance.

## Testing

The `test/shell/` directory contains test scripts to verify correct functionality. Due to the nature of these tests, they must be executed with root privileges to set up test loop devices and load the kernel module.
