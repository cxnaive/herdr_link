#!/usr/bin/env bash
# Install the unchanged GUI dependencies from signed Ubuntu mirrors on CI only.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'This dependency installer is for GitHub runners only.' >&2; exit 1; }
arch=$(dpkg --print-architecture)
case "$arch" in
  amd64) archive=ubuntu; primary=https://archive.ubuntu.com/ubuntu ;;
  arm64) archive=ubuntu-ports; primary=https://ports.ubuntu.com/ubuntu-ports ;;
  *) echo "Unsupported runner architecture: $arch" >&2; exit 1 ;;
esac
work=$(mktemp -d)
trap 'sudo rm -rf -- "$work"' EXIT
# Isolated source/list files avoid the image's broken mirror selection and stale
# indices. Ubuntu's archive key remains mandatory for every fallback mirror.
index=0
for mirror in "$primary" "https://mirrors.edge.kernel.org/$archive" "https://mirrors.tuna.tsinghua.edu.cn/$archive"; do
  index=$((index + 1))
  directory="$work/$index"
  mkdir -p "$directory/lists/partial"
  chmod 755 "$work" "$directory" "$directory/lists" "$directory/lists/partial"
  for suite in noble noble-updates noble-backports noble-security; do
    printf 'deb [arch=%s signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] %s %s main restricted universe multiverse\n' "$arch" "$mirror" "$suite"
  done > "$directory/sources.list"
  options=(-o "Dir::Etc::sourcelist=$directory/sources.list" -o Dir::Etc::sourceparts=- -o "Dir::State::lists=$directory/lists"
    -o Acquire::http::Timeout=15 -o Acquire::https::Timeout=15 -o Acquire::Retries=0 -o APT::Update::Error-Mode=any)
  echo "Installing GUI dependencies from $mirror ($arch)"
  if ! sudo timeout --kill-after=5s 90s apt-get "${options[@]}" update; then
    echo "Mirror metadata unavailable; trying the next signed Ubuntu mirror." >&2
    continue
  fi
  if sudo timeout --kill-after=5s 360s apt-get "${options[@]}" install -y --no-install-recommends \
      libgtk-3-0t64 libwebkit2gtk-4.1-0 libsecret-1-0 gnome-keyring dbus-x11 xvfb xauth xdotool imagemagick fonts-noto-cjk; then
    exit 0
  fi
  echo "Mirror package download failed; trying the next signed Ubuntu mirror." >&2
done
echo 'No signed Ubuntu mirror could install the required GUI dependencies.' >&2
exit 1
