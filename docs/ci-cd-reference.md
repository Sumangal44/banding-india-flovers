# CI/CD reference

A complete reference to the GitHub Actions CI/CD in the upstream `neur0map/ryoku-arch`
repository (`github.com/neur0map/ryoku-arch`), which is the parent of this project.
There are 44 workflows in `.github/workflows/` plus the community files under
`.github/`. Every change to a running machine flows through this pipeline, so it
is the delivery contract made executable: users only ever consume `main`, and a
release is a tagged, frozen copy of `main` published to the signed `[ryoku]`
pacman repo and then baked into an ISO.

## Channel model

`unstable-dev` is the integration branch where CI does its work (testing channel,
rolling unstable pre-release, auto version bumps, auto translations). `main` is
the stable channel and advances only by deliberate fast-forward; a `v*` tag cut
from `main` is a release. `publish-repo.yml` is the only publisher: `x86_64/` is
the stable pointer, `releases/<tag>/x86_64/` is one frozen immutable copy per
release, `channels/testing/x86_64/` is rebuilt on every `unstable-dev` push,
and `releases/index.json` is the release ledger.

## Release pipeline

### stable-release.yml
- **Triggers:** manual dispatch on `main` (inputs: `bump_type` none/patch/minor/major, `release_stage` keep/alpha/beta/stable).
- **Concurrency:** group `stable-release`, no cancel.
- **Steps:** check out `main` with full history; configure bot identity; compute
  the version (`none` tags the current `VERSION`, a bump rewrites `VERSION` on
  main via `bin/ryoku-release-bump`); refuse an existing tag; commit `VERSION`
  if it changed; create the annotated `v*` tag and push it; dispatch
  `publish-repo.yml` on the tag (a `GITHUB_TOKEN` push does not fire workflows);
  dispatch `release-notes.yml` in stable mode (tolerated if absent on an older main).
- **Permissions:** contents + actions write.

### unstable-version-bump.yml
- **Triggers:** push to `unstable-dev` (no manual).
- **Concurrency:** group `unstable-version-bump`, no cancel.
- **Steps:** check out `unstable-dev`; if `VERSION` was hand-edited in the push,
  leave it; otherwise bump via `bin/ryoku-release-bump roll keep` (patch each
  push, rolling to the next minor past 9, preserving the beta line); commit as
  `github-actions[bot]`, rebase and push.
- **Permissions:** contents write.

### publish-repo.yml
- **Triggers:** push `unstable-dev` (testing), tag `v*` (stable), dispatch with
  optional `force_release` input.
- **Concurrency:** per-channel group `publish-repo-stable` / `publish-repo-testing`;
  stable never cancelled, testing superseded by the next push.
- **Jobs:**
  - `build` — in an `archlinux:latest` container: verify the `R2_*`/`GPG_PRIVATE_KEY`
    secrets; install git; checkout with full history (package version derives from
    commit count); resolve the channel from the ref and **refuse anything that is
    not a `v*` tag or `unstable-dev`**; install the shared toolchain from
    `release/repo/build-toolchain.packages` + `rclone`; snapshot the published
    package set from the bucket (`rclone`, the public domain 403s datacenter
    runners) so live names keep their live bytes, mirroring stable then testing
    (excluding `ryoku-desktop` from testing adoption since it carries the channel
    marker); verify each `depends` resolves via `pacman -Si`/`-Sp` against the
    official repos or the internal `[ryoku]` package list; build the repo as a
    throwaway `builder` user with an ephemeral `GNUPGHOME`, GPG loopback pinentry,
    and a 0600 key file; upload the signed repo as an artifact
    (`if-no-files-found: error`, retention 3 days, compression 0).
  - `container-install` — the gate; matrix `base: [arch, cachyos]` on
    `archlinux:latest` / `cachyos/cachyos:latest`; downloads the artifact and runs
    `installation/tests/container-install.sh` which installs `ryoku-desktop` from
    *that* artifact and materializes a full config, verifying signatures exactly as
    a user's pacman does.
  - `publish` — runs only after both pass; downloads the same artifact; configures
    rclone for the bucket; uploads packages and sigs first, `ryoku.db*`/`ryoku.files*`
    last (order matters: a live db with a missing `.sig` breaks `SigLevel=Required`
    installs), then syncs/prunes; for stable, publishes the directory then moves the
    stable pointer; **verifies the bucket**: every `%FILENAME%`/`%CSIZE%` the served
    db lists must exist at the recorded size and have its `.sig` (via `bsdtar`);
    for stable, rebuilds `releases/index.json` from the bucket with
    `bin/ryoku-release-ledger` and asserts the new release is `latest`; then
    dispatches both `build-iso.yml` and `build-iso-cachyos.yml` on the tag with
    `repo_url=https://repo.ryoku.dev/stable/<prefix>/x86_64` so each ISO bakes
    exactly that frozen release.
- **Permissions:** contents read, actions write.

### release-notes.yml
- **Triggers:** push `unstable-dev`, push `v*` tag, dispatch (inputs `mode`,
  `ref`).
- **Concurrency:** per-ref group, no cancel.
- **Steps:** resolve mode/ref; for stable: version from the tag, notes from the
  previous stable tag via `bin/ryoku-release-notes`, CODENAME titling with the
  line's story from `release/names.md` when the name changes, `--prerelease` for
  alpha/beta/rc; create or edit the GitHub release with `--verify-tag`; announce
  to Discord in a branded embed carrying the changelog verbatim (webhook failure
  is non-fatal); for unstable: a single rolling `unstable` pre-release over
  everything since the last stable tag, deleted and recreated each bump.
- **Permissions:** contents write. Secrets: `DISCORD_WEBHOOK_URL`.

### release-ledger.yml
- **Triggers:** manual dispatch.
- **Steps:** in an Arch container, install rclone+jq, configure the bucket, and
  rebuild `releases/index.json` from the bucket with `bin/ryoku-release-ledger`.
  Derived and idempotent, so a rebuild is always safe.

### release-channel-versions.yml
- **Triggers:** PR/push touching `VERSION`, `bin/ryoku-release-bump`,
  `bin/ryoku-release-version`, or the release workflows; dispatch.
- **Jobs:** `validate` — regex-checks the stable and unstable versions and the
  next patch/minor bumps and surfaces them; `mark-unstable` — on `unstable-dev`
  pushes, force-moves the `unstable-dev-latest` tag onto the new HEAD.

### delivery-check.yml
- **Triggers:** push `unstable-dev`/`main`, PR, dispatch.
- **Steps:** full-history checkout; run `bin/ryoku-dev-verify-delivery` (a hard
  fail when a `ryoku/apps` config is not shippable by a package/installer/deploy
  path); annotate how far `main` lags `unstable-dev` (warning, since `main`
  advances by deliberate fast-forward).

### ryotunes-release.yml
- **Triggers:** `repository_dispatch` of type `ryotunes-release` (dispatched by
  the Ryotunes repo's own release workflow), daily cron, manual (input `tag`).
- **Concurrency:** group `ryotunes-release`, no cancel.
- **Steps:** checkout `unstable-dev`; `bin/ryoku-release-ryotunes <tag>` rewrites
  the PKGBUILD (tarball, sha256, pkgver, install hook, changelog); if changed,
  verify the PKGBUILD parses and the tarball sha256 holds against the real URL;
  commit with a `Note: New:` trailer; dispatch `publish-repo.yml` on
  `unstable-dev` explicitly (a `GITHUB_TOKEN` push triggers nothing).

## ISO build

### build-iso.yml
- **Triggers:** tag `v*`, manual dispatch (inputs `release_stage`, optional `repo_url`).
- **Steps:** call the reusable builder with `variant: plain`.
- **Permissions:** contents read, security-events write.

### build-iso-cachyos.yml
- **Triggers:** tag `cachyos-v*`, manual dispatch.
- **Steps:** call the reusable builder with `variant: cachyos`. A separate
  workflow means its own run counter, so the cachyos tracking id is independent
  of the plain one; it publishes `latest-cachyos.json`/`latest-cachyos.js`
  alongside `latest.*`.

### build-iso-reusable.yml
- **Triggers:** `workflow_call` with `variant` (plain| cachyos), `release_stage`,
  and `repo_url` (empty = the moving stable pointer).
- **Permissions:** contents read, security-events write.
- **Job `preflight`** — the install regression gate, run in seconds before the
  multi-hour build: checkout, set up Go from `installation/tui/go.mod`, install
  ShellCheck (apt with a bounded retry, falling back to the upstream release
  tarball), then `bash installation/tests/iso-preflight.sh` (the single source of
  truth; the same script runs on a dev box before dispatching a build).
- **Job `build`** (needs preflight, timeout 120 min):
  1. Checkout with full history, credentials not persisted.
  2. Prepare release metadata: tracking id `r<run>-<shortsha>`, commit, run id,
     channel from stage, installer ref (tag on release, `main` otherwise), variant,
     and `RYOKU_ISO_PUBLIC_BASE=https://iso.ryoku.dev/stable`.
  3. Verify required secrets: `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`,
     `R2_ENDPOINT`, `GPG_PRIVATE_KEY`.
  4. Free runner disk (strip toolchains, `docker system prune`).
  5. Build in a **privileged `archlinux:latest` docker container**: refresh the
     keyring first, upgrade + install `archiso git go base-devel cmake ninja`
     + the Qt6 set + `hyprcursor xcur2png`; mark the repo safe; build
     `ryoku-cursors` locally with `makepkg --nodeps` into a local repo (the
     `[ryoku]` repo 403s datacenter IPs) and hand it to `build.sh` via
     `RYOKU_ISO_LOCAL_REPO`; run `iso-stage-check.sh` (the reproducibility gate)
     before the full build; `./build.sh`; hand the ISO back to the runner user.
     `mkarchiso` work trees live on a host bind mount; `SOURCE_DATE_EPOCH` makes
     the version/label reproducible.
  6. Rename to `ryoku-<date>-r<run>-<sha>-x86_64-<ref>[-cachyos].iso`.
  7. Mount the ISO, extract the SquashFS rootfs, and rsync a readable scan copy.
  8. **Provenance check**: the payload stamp at `/usr/share/ryoku/.payload` must
     carry this run's exact commit and the variant, and the filename must carry
     this run's short sha — a green build of a stale ISO fails here.
  9. Trivy rootfs SARIF (HIGH+CRITICAL, `vuln,secret,misconfig`, skips the mount
     points, pacman cache and brand assets) uploaded as `category: trivy-iso`,
     plus a blocking CRITICAL scan.
  10. GPG sign with an ephemeral keyring (`GNUPGHOME` under a runner temp dir),
     verify, and export the public key next to the ISO.
  11. Generate the release manifest with `bin/ryoku-iso-manifest` (`.sha256`,
      `.json`, `.js`, `latest.json`/`latest.js`).
  12. Install rclone (via `ryoku-r2-config`) and upload the ISO, sig, checksums,
      per-ISO manifests, and the mutable `latest.*` pointers (short cache;
      per-ISO files are immutable and keep the long cache) plus the pubkey to
      `iso.ryoku.dev/stable/`.
  13. Upload ISO artifacts (14-day retention).
  14. Notify Discord on success and failure (never fails the build).

## Installer and system correctness gates

### install-test.yml
- **Triggers:** weekly schedule (`0 6 * * 1`), after a successful "Build ISO"
  `workflow_run`, and manual dispatch. Never on push — it is heavy.
- **Job `container-install`** (matrix Arch/CachyOS, skip unless the triggering
  Build ISO succeeded): in a container, install git, checkout with history, run
  `installation/tests/container-install.sh` (build packages, install
  `ryoku-desktop`, materialize and assert a complete config).
- **Job `vm-install`**: checkout; bounded apt install of QEMU, OVMF, pexpect;
  enable KVM via a udev rule; fetch the built ISO artifact from the triggering
  Build ISO run (or the newest successful one), **refusing a mismatch when the
  triggering commit is known** (the ISO's filename must carry the trigger's short
  sha); build the `[ryoku]` repo for the guest inside a container (sharing
  `release/repo/build-toolchain.packages`); boot the ISO under QEMU with `/dev/kvm`
  and run `installation/tests/install-vm.py` unattended against a virtual disk,
  then verify the installed tree; on a runner without KVM it warns and skips
  cleanly; upload the serial log always (`if-no-files-found: ignore`).

### install-backend-tests.yml
- **Triggers:** path-filtered PR/push on `installation/**`, `system/boot/limine/**`,
  `tests/install-*.sh`, PKGBUILDs, driver paths, `system/packages/*.packages`, etc.
- **Job `unit`** (no root, 20-min bound): whole-disk wipe guard (`install-partition-whole.sh`),
  largest free-space sizer, Secure Boot preflight gate, clock-skew heal, dry-run
  step/sentinel matrix, offline install path, non-interactive install, initramfs
  hook masking, offline package builds resolve no network deps, offline repo
  integrity (no truncated packages), live installer display scale, Limine boot-menu
  config handling, Windows chainload entry, disk teardown before the pre-wipe, DNS
  preflight gate, ASUS Aura hardware detection.
- **Job `loop-device`** (real loop device, needs sudo): install disk tools
  (`gdisk parted dosfstools btrfs-progs ntfs-3g jq`, bounded+retried); partition
  alongside a fake Windows layout via `losetup`/`sgdisk`; carve space out of
  ntfs/ext4/btrfs (`install-resize.sh`); `@swap` swapfile activation.
- **Job `tui`**: Go unit tests in `installation/tui`.
- **Job `chainload-vm`**: real QEMU+OVMF boot of the generated `limine.conf`
  (`install-chainload-vm.sh`), gated on `/dev/kvm`; hosted runners lack KVM so it
  emits a visible `::notice` and skips; runs for real on a KVM-capable/self-hosted
  runner or via dispatch.

### install-chroot-safety.yml
- **Triggers:** PR/push touching `installation/backend/**` or the test.
- **Steps:** `tests/install-chroot-safety.sh` — every `systemctl --user` enable/
  start in a chroot-run phase must be guarded with `|| true` (no user bus in the
  chroot; the install otherwise halts before `login/` and `post-install/`).

### install-mirrors.yml
- **Triggers:** PR/push touching `installation/backend/**` or the test.
- **Steps:** `tests/install-mirrors.sh` — reflector ranking must always fall back
  to the shipped mirror list; blocks the "Operation too slow. Less than 1
  bytes/sec" pacstrap failure class.

## Lint, unit and static-analysis gates

### go-unit-tests.yml
- **Triggers:** path-filtered PR/push on `ryoku/cli/**`, `ryoku/shell/ipc/**`,
  `ryoku-shell-installer/**`, and the store/hub/rashin backends; dispatch.
- **Jobs** (each: checkout, `setup-go` from the module's `go.mod`, `go build`,
  `go vet`, `go test`): CLI (update/doctor/materialize), shell IPC daemon
  (`go test -short`), shell installer (with an extra committed-binary checksum
  parity step that runs **before** `go build`, so a commit updating the binary
  without the `.sha256` fails), store backend, hub backend, rashin backend.

### shellcheck.yml
- **Triggers:** path-filtered PR/push for `**/*.sh`/`**/*.bash`, `bin/`,
  `installation/`, `system/`, `ryoku/`, `tests/`, `.githooks/`, workflows; dispatch
  with `scope` changed|all.
- **Steps:** checkout full history; install ShellCheck (apt with retries, bounded,
  falling back to the upstream release tarball); collect changed shell files via
  the git stream (skipping qylock, vendored trees, and shebang-mismatched scripts);
  run `shellcheck -x -s bash --severity=warning`.

### ai-slop.yml
- **Triggers:** PR, push to `main`, dispatch (scope changed|all).
- **Steps:** `bin/ryoku-dev-scan-slop --range <base> <head>` over added lines only,
  mirroring the pre-commit check that fails AI-slop comments (placeholder stubs,
  chat residue, attribution, self-narration).

### inclusive-language.yml
- **Triggers:** path-filtered PR/push for text-like extensions and the workflow
  files; dispatch.
- **Steps:** install `woke v0.19.0` via Go; collect changed files by extension
  plus MIME type (skipping `ryoku/assets/*`, `qylock`, `docs/media/*`); run
  `woke --config .woke.yml --exit-1-on-failure --output github-actions`.

### qmllint.yml
- **Triggers:** path-filtered PR/push for `**/*.qml`; dispatch.
- **Steps:** install `qt6-declarative-dev` (retried); collect changed QML (vendored
  `qylock`/`wall-ui` exempt); run `qmllint` per file **as an advisory only** —
  Quickshell modules are not installable on a CI runner, so it never blocks.

### shell-unit-tests.yml
- **Triggers:** path-filtered PR/push for `ryoku/shell/**` and `ryoku/hub/**`
  JS/MJS plus the runner and this workflow; dispatch.
- **Steps:** Node 20; `tests/shell-unit-tests.sh` — a real blocking gate that
  discovers every `*.test.mjs` (launcher fuzzy ranker, ryoshot coords/keymap/
  annotation libs, hub display-arrangement lib); no display or Quickshell needed.

### shell-ipc-parity.yml
- **Triggers:** path-filtered PR/push for `ryoku/hyprland/**`, `ryoku/shell/ipc/**`,
  `bin/**`; dispatch.
- **Steps:** `bin/ryoku-dev-audit-shell-binds --check` — fails when a keybind
  dispatches to a `ryoku-shell ipc <target> <fn>` the shell does not register
  (a silent no-op key), against the committed `ipc-surface.txt` manifest.

### shell-tool-availability.yml
- **Triggers:** path-filtered PR/push for the package sets or the test; dispatch.
- **Steps:** `tests/shell-tool-availability.sh` — every external program
  `ProgramCheckerService` gates on must map to a shipped package or be
  allow-listed as bring-your-own.

### docs-sync.yml
- **Triggers:** path-filtered PR/push for `**/*.md`; dispatch.
- **Steps:** for every tracked `.md` (vendored trees skipped), resolve each
  relative link (dropping anchors and title suffixes); fail on any broken link.

### sddm-wayland.yml
- **Triggers:** path-filtered PR/push for the SDDM setup, doctor, base package set,
  `ryoku-desktop` PKGBUILD, or the test; dispatch.
- **Steps:** `go test -run TestSDDMWaylandBodyForcesQtWayland` in the CLI, then
  `tests/sddm-wayland.sh` for the installer and packages — the greeter must use
  Qt Wayland end to end.

### snapshot-cleanup.yml
- **Triggers:** path-filtered PR/push for the updater/doctor/snapshot code; dispatch.
- **Steps:** install `snapper` + `btrfs-progs`; `sudo bash tests/snapshot-cleanup.sh`
  drives **real snapper** on a loop-device btrfs — reproduces the runaway snapshot
  pile, proves `snapper cleanup number` bounds it to `NUMBER_LIMIT`, and proves the
  doctor drain removes leaked timeline snapshots that number cleanup skips.

### stash-install.yml
- **Triggers:** path-filtered PR/push for `stash-install.sh` or the test; dispatch.
- **Steps:** install `libarchive-tools` (bsdtar); `tests/stash-install.sh` stubs
  pkexec/pacman/flatpak and checks each dispatch (`.pkg.tar.zst` → `pacman -U`,
  flatpak bundle → `flatpak install`, `.deb/.rpm` → bsdtar) and that a generic
  tarball is never escalated.

### controllers.yml
- **Triggers:** path-filtered PR/push for `system/hardware/input/**`, the package
  sets, the controller package recipes, or the test; dispatch.
- **Steps:** `tests/controllers.sh` pins the delivery half of controller/Bluetooth
  support: xpadneo + the ERTM drop-in ship in base (never AUR — `ryoku update`
  could not reach them), the ERTM option cannot silently flip back, and xone is
  never installed by default (it blacklists xpad and mt76x2u).

### bluetooth-audio.yml
- **Triggers:** path-filtered PR/push for the WirePlumber app config, `ryoku-bt-audio`,
  or the tests; dispatch.
- **Steps:** install jq; `tests/bt-audio-codec.sh` (codec memory driven against a
  fake `pactl`: refuses an unoffered codec, leaves a correct device alone, restores
  the remembered codec after a reconnect); install pipewire-bin + wireplumber and
  run `tests/bluetooth-audio-policy.sh` (asserts `sbc_xq` ranks above `aac` by
  POSITION and the mic profile switch stays on).

### game-mode.yml
- **Triggers:** path-filtered PR/push for `ryoku-cmd-game-mode`, `ryoku-game-tune`,
  its udev rules, or the tests; dispatch.
- **Steps:** `tests/game-tune.sh` — the privileged half against a fake `/proc` and
  cpuidle tree: every knob the kernel exposes is moved, deep idle states are picked
  by exit cost, restore replays the exact prior values (not defaults), a second
  apply cannot record tuned values as originals, and a seamed (redirected-path)
  run refuses to escalate (pkexec strips the environment). Then `tests/game-mode.sh`
  for the toggle wiring.

### nvidia-guard.yml
- **Triggers:** path-filtered PR/push for `ryoku-nvidia-guard`, `nvidia.sh`, or the
  tests; dispatch.
- **Steps:** stubbed pacman/modinfo/mkinitcpio — no GPU, no root needed:
  `tests/nvidia-guard.sh` (the heal restores nouveau when nouveau is blacklisted
  and no nvidia module exists for any installed kernel; stays a no-op on a healthy
  box, non-NVIDIA box, or mid-install) and `tests/nvidia-driver-selection.sh`.

### monitor-profiles.yml
- **Triggers:** path-filtered PR/push for `ryoku-monitor` or the test; dispatch.
- **Steps:** install jq + lua5.4; `tests/monitor-profiles.sh` in fixture mode, no
  compositor: explicit layout apply keeps chosen modes (not highest refresh),
  hardware-keyed named profiles, and identity remap that survives a connector
  rename.

### cobalt-setup.yml
- **Triggers:** path-filtered PR/push for `system/containers/**`, the stash cobalt
  server script, or the test; dispatch.
- **Steps:** `tests/cobalt-setup.sh` pins the negative cases that matter for the
  one privileged docker door: a bad port or unknown verb is refused **before**
  escalation, there is deliberately no docker passthrough, and
  `stash-cobalt-server.sh` picks the right door (direct docker when available,
  the helper otherwise, honest "denied" when neither exists).

### localsend-receive.yml
- **Triggers:** path-filtered PR/push for `localsend.sh` or the test; dispatch.
- **Steps:** `tests/localsend-receive.sh` drives the receive server over HTTPS and
  feeds ACCEPT/DECLINE verdicts on stdin: ACCEPT grants the upload token, DECLINE
  or silence returns 403, no bytes move.

### ryoku-recovery.yml
- **Triggers:** path-filtered PR/push for `bin/ryoku-recovery` or the test; dispatch.
- **Steps:** `tests/ryoku-recovery.sh` — the curl|bash panic button must always
  restore a machine to stable `main` and repair the broken checkout in place, even
  when `RYOKU_CHANNEL` has leaked to `unstable-dev` (an old ISO updater switching
  the checkout to `unstable-dev` bricked users because the rewritten tree lacked
  the old helper commands).

## Security scanning

### trivy.yml
- **Triggers:** push/PR on `main`, weekly schedule (`17 5 * * 2`), dispatch.
- **Permissions:** contents read, security-events write.
- **Steps:** filesystem SARIF (`vuln,secret,misconfig`, HIGH+CRITICAL, unfixed
  ignored, skips `.git`, `ryoku/assets`, `docs/media`, iso work/out dirs) uploaded
  via the CodeQL uploader (skipped for forks); blocking pass for high-confidence
  secrets; blocking pass for CRITICAL vulnerabilities and misconfigurations.
- **Pins:** `aquasecurity/trivy-action` at commit for v0.36.0, `trivy` v0.70.0.

### codeql.yml
- **Triggers:** push/PR on `main`, weekly schedule (`34 4 * * 1`), dispatch.
- **Matrix:** `actions` and `javascript-typescript`, both build-mode none.
- **Permissions:** actions + contents read, security-events write.

### pr-shell-script-alert.yml
- **Triggers:** PR opened/synchronize/reopened/ready_for_review.
- **Steps:** list changed files via the API; flag any that is a `.sh` file, a
  security-sensitive path (`.github/`, `.githooks/`, `bin/`, `tests/`,
  `installation/`, `system/`, shell IPC, deploy script, hyprland scripts), or has
  a shell shebang; warn with annotations and a summary table plus review focus.
  **Advisory only — never blocks merge.**

## Community and notifications

### commit-notifications.yml
- **Triggers:** push to `main` or `unstable-dev`.
- **Concurrency:** per-branch group, no cancel.
- **Steps:** keep a per-branch pointer `refs/notify/commits/<branch>`; count
  unnotified commits since it; only notify once the count hits the threshold (5);
  build a tag-grouped description (`[area]` prefixes, newest first, per-tag cap of
  8, `… +N more`); branch-colored embed green for main / amber for unstable-dev
  with a compare URL; advance the pointer after a successful send. The pointer
  namespace never re-triggers this workflow.

### discord-notifications.yml
- **Triggers:** `issues: opened`, `pull_request_target: opened`, dispatch with
  preview inputs.
- **Steps:** issue intake embeds (labels, signal, maintainer focus), PR review
  embeds (branch, base), and release-changelog embeds chunked to Discord's limits
  with per-part footers. Preview mode sends canned issue/PR/release versions.

### project-auto-add.yml
- **Triggers:** issues/prs opened or reopened.
- **Steps:** if `PROJECTS_TOKEN` and `PROJECT_URL` are configured, add the item to
  the Ryoku Roadmap board via `actions/add-to-project@v1.0.2` (the default
  `GITHUB_TOKEN` cannot touch Projects v2).

### project-triage.yml
- **Triggers:** issues/prs opened or edited.
- **Steps:** read event fields from the payload; gather changed paths for PRs; a
  deterministic path/label heuristic (`Area`/`Type`); an AI pass via GitHub Models
  (`openai/gpt-4o-mini`, JSON-validated, any failure quietly keeps the heuristic);
  ensure the item is on the board (idempotent GraphQL), set the Area/Type board
  fields, and mirror `area:*`/`type:*` repo labels. Classification never fails the
  run — any miss is a skip.

### project-roadmap-export.yml
- **Triggers:** 3-hourly schedule, dispatch, `repository_dispatch` type
  `roadmap-refresh`.
- **Steps:** query the board (paginated GraphQL) into `nodes.ndjson`; transform
  into the stable `roadmap.json` shape (ordered columns, Done limited and sorted,
  stats); commit back to `main` only when changed.

### i18n.yml
- **Triggers:** push to `beta-18`/`unstable-dev` path-filtered on `**/*.qml`,
  hub schema JS, `i18n-sync.py`, overrides, or the workflow; dispatch with a
  `force` input.
- **Concurrency:** per-ref group, cancel-in-progress.
- **Steps:** `i18n-sync.py extract`; `sync` translating only missing strings by
  default (`--force` re-translates everything) — the LLM engine via OpenRouter
  (`google/gemini-2.5-flash` model) when `RYOKU_I18N_KEY` is set, otherwise the
  keyless Google endpoint; advisory expansion check; commit to the pushed branch
  (preserving `overrides/<lang>.json`), rebase-retry on race with the version bump.

## `.github/` community files

- `FUNDING.yml` — `github: neur0map`, `ko_fi: ryokuarch`.
- `PULL_REQUEST_TEMPLATE.md` — What changed / Area / How it was tested / checklist
  (one logical change, CHANGELOG updated, no `--no-verify`, Lua/shell/QML gates,
  no duplicated config/dead/commented code, no em-dash, docs updated).
- `ISSUE_TEMPLATE/bug.yml` — structured bug form (System details; What's wrong,
  ask for `ryoku debug` output), label `bug`.
- `ISSUE_TEMPLATE/config.yml` — blank issues disabled; suggestions point to the
  Discussions board.

## Secrets and variables

- **Secrets:** `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, `R2_ENDPOINT`,
  `R2_BUCKET`, `GPG_PRIVATE_KEY`, `GPG_PASSPHRASE`, `DISCORD_WEBHOOK_URL`,
  `DISCORD_COMMIT_WEBHOOK_URL`, `PROJECTS_TOKEN`, `RYOKU_I18N_KEY`.
- **Variables:** `PROJECT_URL`, `PROJECT_NUMBER`, `RYOKU_I18N_MODEL`.
- `GPG_PRIVATE_KEY` is always imported into an ephemeral `GNUPGHOME` under a
  runner temp directory and discarded; it never touches persistent storage.
- rclone config is always generated by `bin/ryoku-r2-config` in-place; anything
  reading from Cloudflare talks to the bucket through rclone because the public
  domain 403s datacenter runner IPs.

## Key invariants this pipeline enforces

1. **Build once, test those bytes, publish those bytes** — `publish-repo` gates on
   a container install of the exact artifact before uploading, and its verification
   step re-checks the bucket against the served db.
2. **Packages first, db last** — a live `ryoku.db` never outruns its packages and
   sigs (issue #21: a missing `.sig` returns an HTML 404 that overruns pacman's
   signature cap and breaks `SigLevel=Required` installs).
3. **A release is published once** — `releases/<tag>/` is never overwritten
   (unless `force_release` is explicitly set).
4. **Provenance over trust** — the ISO payload carries a commit/variant stamp, the
   filename carries the short sha, and the VM install refuses to test an ISO from
   the wrong commit. A green build against a stale image fails rather than certifies.
5. **Gates before cost** — ISO builds run a seconds-long preflight before a
   multi-hour mkarchiso; VM/loop-device tests run on heavy, schedule-or-dispatch
   triggers; path-filtering keeps per-area tests cheap.
6. **`unstable-dev` is where integration breaks are caught** — Go unit tests,
   publish, delivery-check, translations and version bumps all run there, because
   that is the branch where integration actually happens.