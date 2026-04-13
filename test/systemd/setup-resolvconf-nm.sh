#!/bin/bash
# Unmount Docker's bind-mounted /etc/resolv.conf so NetworkManager can manage it.
# NM writes resolv.conf via a temp file + rename(); rename() over a bind-mount
# target fails with EBUSY, so NM's writes would silently fail without this.
#
# After unmounting, seed a placeholder resolv.conf so early boot DNS works; NM
# will overwrite it with DNS servers from its managed connection profile once
# NetworkManager.service starts.

umount /etc/resolv.conf 2>/dev/null || true
rm -f /etc/resolv.conf
printf "nameserver 8.8.8.8\nnameserver 1.1.1.1\n" > /etc/resolv.conf
