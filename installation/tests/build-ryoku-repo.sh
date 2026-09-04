#!/usr/bin/env bash
# Build the [ryoku] packages from this checkout into a local pacman repo, signed
# with a throwaway key. Shared by the two install tests: container-install.sh
# registers the repo and installs from it; the VM install serves it to the guest
# over the qemu NAT so the real installer pulls the desktop locally (CI cannot
# reach the public repo: Cloudflare 403s datacenter IPs).
#
# Runs as root. The host toolchain (base-devel go cmake ninja qt6-* gnupg) and
# the base keyring must already be installed. Consumers relax SigLevel to Never,
# so the throwaway key never needs trusting.
#
#   RYOKU_REPO_NAME=<name> (default ryoku)   installation/tests/build-ryoku-repo.sh
set -euo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
name=${RYOKU_REPO_NAME:-ryoku}
out="$REPO/release/repo/out/x86_64"

[[ $EUID -eq 0 ]] || { echo "build-ryoku-repo: must run as root" >&2; exit 1; }

# makepkg refuses to run as root, so build as an unprivileged user. MIRROR points
# at an empty dir so build-repo.sh's "adopt already-published bytes" step is a
# no-op (all fresh, no network).
id builder &>/dev/null || useradd --create-home builder
chown -R builder:builder "$REPO"
mirror_stub=$(mktemp -d)
chmod 755 "$mirror_stub"

runuser -u builder -- env \
  HOME=/home/builder \
  RYOKU_REPO_ROOT="$REPO" \
  RYOKU_REPO_OUT="$REPO/release/repo/out" \
  RYOKU_REPO_NAME="$name" \
  RYOKU_REPO_MIRROR="$mirror_stub" \
  bash -euo pipefail <<'BUILDER'
export GNUPGHOME=$(mktemp -d)
chmod 700 "$GNUPGHOME"
gpg --batch --gen-key <<'KEY'
%no-protection
Key-Type: eddsa
Key-Curve: ed25519
Key-Usage: sign
Expire-Date: 0
Name-Real: Ryoku CI
Name-Email: ci@ryoku.local
%commit
KEY
RYOKU_REPO_KEY=$(gpg --list-secret-keys --with-colons | awk -F: '/^fpr/{print $10; exit}')
export RYOKU_REPO_KEY
"$RYOKU_REPO_ROOT/release/repo/build-repo.sh"
BUILDER

[[ -f "$out/$name.db" ]] || { echo "build-ryoku-repo: no repo db at $out/$name.db" >&2; exit 1; }
echo "build-ryoku-repo: built [$name] at $out"
