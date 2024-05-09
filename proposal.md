# Observational Block Device Replication scheme for Linux (bdr)

A light-weight block device replication scheme for Linux which stays out of the
critical path.

## Proposal for "Ročníkový projekt" and BSc. thesis

Most block device replication schemes for Linux are bulky and complex. We
propose to build a light-weight alternative with the following properties:

- If any part of the replication machinery fails, the primary is unaffected.

- If necessary, we temporarily sacrifice consistency of the replica to guarantee
  availability of the primary (eventual consistency).

- The operation of the replication machinery has no effect, or only negligible
  effect, on the performance of the primary.

To achieve this outcome, we propose to:

- Implement a Device Mapper (DM) target for Linux implementing a very simple
  layer which merely observes the reads and writes are performed against a lower
  layer (an NVMe storage drive, a LUKS container, etc.):

  - The main design goal for the kernel module is to be as simple as possible,
    to the point where the failure of the kernel module is virtually impossible.

  - Reads are passed by the DM target directly onto the device below.

  - Writes are passed by the DM target directly onto the device below, but
    information about writes is additionally propagated by the DM target into
    the user space, where a user space daemon collects it and sends the
    writes (data and metadata) over a Wireguard tunnel.

  - A replication daemon on the remote replays writes against the remote block
    device in a suitable order, effectively synchronizing the primary and the
    replica.

  - In this sense, this replication is "observational", as it merely watches the
    block device for writes, but stays out of the critical path of
    writes.

- When any of the daemons (on the primary or replica side) or the network
  between them fail, in the worst case, a rescan of the primary is required.
  This results from our decision to stay out of the critical path.

  - To avoid having to rescan the primary, a journal or an equivalent data
    structure would be required on the primary, changing its performance,
    durability and reliability characteristics, which we deem unacceptable.

- Special attention is needed to maintain the replica consistent: the state of
  the replica must be a prefix of the state of the primary (prefix consistency).
  Otherwise, we would be unable to guarantee that (for example) a file system on
  the replica is mountable, which defeats the point of replication to a large
  extent.

- The replication should be easy to set up. Especially, it should be possible
  to layer the replication scheme over an existing block device with little or
  no effort.

## Possible extensions for BSc. thesis

The kernel part (the simple custom DM target) can be extended to support
user space implementation of arbitrary DM targets, aided by a user space
library.

This framework called DMUSE (Device Mapper In Userspace) would be akin to BUSE
(Block Device In Userspace, [1]) with a similar set of constraints, performance
imperatives, reliability and performance trade-offs and (dis)advantages.

DMUSE would be useful at least for:

- The aforementioned block-device replication scheme which would be easily
  implemented atop DMUSE.

- Other replications schemes, possibly providing a very different consistency,
  availability, durability, ... trade-offs, which could be based on DMUSE. This
  is where the relative ease of user space development would be most beneficial.

- Logging-only DM targets useful for development and debugging of file systems
  which allow the developer to observe/replay reads and writes carried against a
  block device. See [2] for an existing dedicated facility.

- Fault injection DMUSE target for practical verification of the behavior of
  file systems under power failure. See [3], [4] for existing dedicated
  facilities.

[1]: https://dspace.cuni.cz/bitstream/handle/20.500.11956/148791/120397658.pdf
[2]: https://linux.die.net/man/8/blktrace
[3]: https://www.kernel.org/doc/html/v5.5/admin-guide/device-mapper/dm-dust.html
[4]: https://www.kernel.org/doc/html/v5.5/admin-guide/device-mapper/dm-flakey.html
