# Filesystem Support Notes

`logcut` depends on Linux punch-hole support through the `fallocate` syscall with keep-size behavior.

## Expected good candidates

- XFS
- ext4
- Btrfs, with care around copy-on-write behavior
- tmpfs, mostly for tests

## Risky or environment-dependent candidates

- NFS
- overlayfs
- container bind mounts
- network filesystems
- ZFS on Linux

## How logcut checks support

`logcut --preflight` creates a small temporary file in the source directory, attempts a punch-hole operation, and removes the test file. If that operation fails, the preflight result fails.

## Sparse file behavior

After successful compaction, the active source log becomes sparse. Apparent size and allocated size can differ.

Use allocated-size tools to confirm recovered space. Do not rely only on apparent size.

## Operational recommendation

Run preflight on every new filesystem type before using logcut in production.

For large fleets, record filesystem type and mount details in your CMDB or monitoring inventory.
