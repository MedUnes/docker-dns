#!/bin/bash
# Replace Docker's bind-mounted /etc/resolv.conf with a symlink to the
# systemd-resolved stub, matching a real Ubuntu Noble install.

# Docker bind-mounts /etc/resolv.conf, so we must unmount it first.
umount /etc/resolv.conf 2>/dev/null || true
rm -f /etc/resolv.conf
ln -s /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
