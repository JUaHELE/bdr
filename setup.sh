#!/bin/bash
set -eou pipefail

if [[ $EUID -ne 0 ]]; then
    echo "This script must be run as root to create BDR mapper." >&2
    exit 1
fi


# Constants
WRITE_SIZE=4104  # Size of one write in bytes

error_exit() {
    echo "Error: $1" >&2
    exit 1
}

if ! lsmod | grep "^bdr" > /dev/null ; then
    error_exit "bdr module is not loaded loaded."
fi

# Function to convert size to number of writes
convert_to_writes() {
    local size=$1
    local unit=${size//[0-9.]/}  # Extract unit (K, M, or empty for bytes)
    local value=${size//[^0-9.]/}  # Extract numeric value

    # Convert to bytes based on unit
    local bytes
    case "${unit^^}" in
        "K")
            bytes=$(echo "$value * 1024" | bc)
            ;;
        "M")
            bytes=$(echo "$value * 1024 * 1024" | bc)
            ;;
        "")
            bytes=$value
            ;;
        *)
            error_exit "Unknown unit: $unit. Use K, M or nothing"
            ;;
    esac

    # Calculate number of writes and round up
    local writes=$(echo "($bytes + $WRITE_SIZE - 1) / $WRITE_SIZE" | bc)
    echo $writes
}

read -p "Enter how you want to name your mapper (name of the device in /dev/mapper): " target_name
read -p "Enter replicated device (e.g., /dev/loop0, /dev/sdb2): " device
read -p "Enter character device name (any, but advised similar to mapper name): " chardev_name

# Ask for buffer size with unit and convert to writes
read -p "Enter buffer size (e.g., 4104, 8K, 1M): " buffer_size_input
buffer_size_in_writes=$(convert_to_writes "$buffer_size_input")
echo "Buffer size converted to $buffer_size_in_writes writes"

# Ensure device exists
if [ ! -b "$device" ]; then
    error_exit "Device $device does not exist."
fi

# Create DM target
echo "0 $(blockdev --getsz $device) bdr $device $chardev_name $buffer_size_in_writes" | \
    sudo dmsetup create "$target_name" || \
    error_exit "Can't create target '$target_name' on '$device' with character device '$chardev_name' and buffer size $buffer_size_in_writes. Probably name collision or bdr module isn't loaded."

echo "Device Mapper target '$target_name' on '$device' with character device '/dev/$chardev_name' and buffer size $buffer_size_in_writes created successfully. Your device is available /dev/mapper/$target_name"
dmsetup ls
