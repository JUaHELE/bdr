# BDR: Block Device Replicator

## Introduction
BDR (Block Device Replicator) is a real-time, network-based block device replication solution designed for Linux. BDR observes writes made to a specified device and sends them over the network to a designated destination, where the receiver daemon writes them to a backup device, essentially mirroring RAID 1 functionality over the network.

If the disk experiences high load and the client daemon cannot send new writes quickly enough, a complete disk scan is necessary to ensure consistency. The client daemon performs a full disk scan at startup by default to ensure consistency (this can be disabled if you're certain devices are identical, though this is not recommended).

## Requirements

* GNU Make
* Linux Kernel >= 6.8
* Linux Kernel Headers
* Go >= 1.23.0

## Installation Tutorial

This tutorial will guide you through setting up BDR for local testing using loop devices.

### 1. Clone the Repository

```bash
git clone https://github.com/JUaHELE/bdr.git
cd bdr
```

### 2. Build the Kernel Driver

Build and load the kernel driver:

```bash
cd kern/
make
sudo insmod bdr.ko
```

Verify the module is loaded:

```bash
lsmod | grep bdr
```

### 3. Build the Userspace Daemons

Build both the client and server applications:

```bash
# Build the client daemon
cd user/client
go build -o bdr_client

# Build the server daemon
cd ../server
go build -o bdr_server

# Copy binaries to a convenient location (optional)
sudo cp bdr_client /usr/local/bin/
sudo cp bdr_server /usr/local/bin/
```

### 4. Create Test Loop Devices

For testing purposes, we'll create two loop devices to use as source and destination:

```bash
# Create 1GB test files
sudo dd if=/dev/zero of=/tmp/source_disk.img bs=1M count=1024
sudo dd if=/dev/zero of=/tmp/target_disk.img bs=1M count=1024
sudo dd if=/dev/zero of=/tmp/journal_disk.img bs=256M count=1

# Create loop devices from these files
sudo losetup /dev/loop0 /tmp/source_disk.img
sudo losetup /dev/loop1 /tmp/target_disk.img
sudo losetup /dev/loop2 /tmp/journal_disk.img
```

### 5. Setup the Kernel Module

To create the device mapper target, control device and allocate buffer space use following script. As mapper name enter `bdr_source`, character device name `bdr_control` and buffer size for example `20M`.

```bash
sudo ./setup.sh
```

Now we have:
- Source device available at: `/dev/mapper/bdr_source`
- BDR control device at: `/dev/bdr_control`
- Source device at: `/dev/loop0`
- Target device at: `/dev/loop1`
- Journal device at: `/dev/loop2`

### 6. Start the Server

Start the server first:

```bash
sudo bdr_server --address 127.0.0.1 --port 8000 --target-device /dev/loop1 --journal /dev/loop2 --verbose
```

### 7. Start the Client

In a new terminal, start the client:

```bash
sudo bdr_client --control-device /dev/bdr_control --source-device /dev/mapper/bdr_source --address 127.0.0.1 --port 8000 --verbose
```

The replication begins automatically. The client will perform an initial full scan to ensure consistency.

### 8. Test the Replication

After the initial scan you can test the replication by writing to the source device and verifying the changes appear on the target:

```bash
sudo mkfs.btrfs /dev/mapper/bdr_source

# Mount the source
sudo mkdir -p /mnt/source
sudo mount /dev/mapper/bdr_source /mnt/source

# Create some test files
sudo dd if=/dev/urandom of=/mnt/source/test1.bin bs=1M count=5
sync

sudo dd if=/dev/urandom of=/mnt/source/test2.bin bs=1M count=5
sync

# Unmount
sudo umount /mnt/source

# Mount target and verify
sudo mkdir -p /mnt/target
sudo mount /dev/loop1 /mnt/target
ls -la /mnt/target
sudo umount /mnt/target
```

### 9. Cleanup

When you're done, clean up the environment:

```bash
# Stop both client and server daemons (Ctrl+C)

# Remove the device mapper
sudo dmsetup remove bdr_source

# Unload the kernel module
sudo rmmod bdr

# Remove loop devices
sudo losetup -d /dev/loop0
sudo losetup -d /dev/loop1
sudo losetup -d /dev/loop2

# Remove the test files
sudo rm /tmp/source_disk.img /tmp/target_disk.img /tmp/journal_disk.img
```

## Command Line Options

### Client Options

```
  --address string       Receiver IP address (required)
  --control-device       Path to BDR control character device (required)
  --full-scan            Initiates direct full scan after start of the daemon
  --full-replication     Initiates full replication after start of the daemon
  --no-replication       Disables replication when started
  --port int             Receiver port (required)
  --source-device        Path to BDR source device mapper (required)
  --verbose              Provides verbose output of the program
  --debug                Provides detailed debug information
```

### Server Options

```
  --address string         IP address to listen on (required)
  --port int               Port to listen on (required)
  --target-device string   Path to replica destination device (required)
  --journal string         Path to journal device (required)
  --verbose                Provides verbose output of the program
  --debug                  Provides debug output of the program
```

## Running Tests

The project includes test scripts in the `test/shell/` directory and can be run independently or with dedicated script `run_all_tests.sh`:

```bash
cd test/shell/
sudo ./run_all_tests.sh
```

Tests can be run independently, expected output will show the test results with "TEST PASSED" or "TEST FAILED" prints for each test case. The `run_all_tests.sh` will summarize the test results.
