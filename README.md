# BDR: Block Device Replicator

## Introduction
BDR (Block Device Replicator) is a real-time, network-based block device replication solution designed for Linux. BDR observes writes made to a specified device and sends them over the network to a designated destination, where the receiver daemon writes them to a backup device—essentially mirroring RAID 1 functionality. BDR is best suited for disks with low workloads or slower disks, as the kernel can process data faster than the user-space daemon.

## Reqirements

* GNU Make
* Linux Kernel >= 6.8
* Linux Kernel Headers
* go >= 1.19

