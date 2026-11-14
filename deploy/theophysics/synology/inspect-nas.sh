#!/bin/sh
# Read-only Synology deployment inventory. Run on the NAS, not in the cloud.
# Does not read credentials, alter files, install packages, or change services.
set -u
PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export PATH
section() { printf '\n[%s]\n' "$1"; }
printf 'THEOPHYSICS_NAS_INSPECTION_VERSION=1\n'
section 'System'
printf 'Architecture: '; uname -m
printf 'Kernel: '; uname -sr
printf 'User ID: '; id -u
if [ -r /proc/sys/kernel/syno_hw_version ]; then
  printf 'Synology model: '; cat /proc/sys/kernel/syno_hw_version; printf '\n'
fi
if [ -r /etc/VERSION ]; then
  awk -F= '$1 ~ /^(majorversion|minorversion|productversion|buildnumber|smallfixnumber)$/ { print "DSM " $0 }' /etc/VERSION
fi
if [ -r /proc/meminfo ]; then
  awk '/^(MemTotal|MemAvailable|SwapTotal):/ { print }' /proc/meminfo
fi
section 'Mounted data volumes (capacity only)'
found=0
for volume in /volume[0-9]* /volumeUSB[0-9]*; do
  [ -d "$volume" ] || continue
  found=1
  df -Pk "$volume" 2>/dev/null || true
  ls -ldn "$volume" 2>/dev/null || true
  if [ -d "$volume/docker" ]; then
    printf 'Existing container parent directory: '
    ls -ldn "$volume/docker" 2>/dev/null || true
  fi
done
[ "$found" -eq 1 ] || printf 'No Synology-style data volumes found. Was this run on the NAS?\n'
if [ -r /proc/mounts ]; then
  awk '$2 ~ /^\/volume/ { print "Filesystem:", $1, $2, $3 }' /proc/mounts
fi
section 'RAID state (reported by the kernel)'
if [ -r /proc/mdstat ]; then cat /proc/mdstat; else printf 'Not available.\n'; fi
section 'Container runtime'
if command -v docker >/dev/null 2>&1; then
  docker version --format 'Docker client {{.Client.Version}}; server {{if .Server}}{{.Server.Version}}{{else}}unavailable{{end}}' 2>&1 || true
  docker info --format 'Server architecture {{.Architecture}}; CPUs {{.NCPU}}; RAM bytes {{.MemTotal}}' 2>&1 || true
  if docker compose version >/dev/null 2>&1; then
    docker compose version
  elif command -v docker-compose >/dev/null 2>&1; then
    docker-compose version --short 2>&1 || true
  else
    printf 'Compose is unavailable to this account.\n'
  fi
  printf 'Existing container names and published ports (no environment values):\n'
  docker ps --format '{{.Names}} | {{.Ports}}' 2>&1 || true
else
  printf 'Docker is unavailable to this account; check DSM Container Manager.\n'
fi
section 'Storage selection still required'
printf '%s\n' \
  'Confirm volume/pool health and SSD pool availability in DSM Storage Manager.' \
  'Choose separate new directories for documents, PostgreSQL, and Hister indexes.' \
  'No paths, permissions, container settings, or data were changed by this script.'
