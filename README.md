# redis-tool - Redis Cluster Lifecycle Tool

A CLI (Go) that wraps Ansible to provision, operate, and rolling-upgrade a
6-node Redis Cluster (3 masters + 3 replicas) running inside containers. The
upgrade achieves **zero client-visible downtime** and **verified data
integrity** - pre-upgrade and post-upgrade `data verify` both report
`PASS - 1000/1000`.

```bash
redis-tool
├── doctor                          # standalone prereq check
├── infra        up | down | status # lifecycle of the 6 containers
├── provision    --version X.Y.Z    # install + start + form cluster
├── data         seed | verify      # deterministic SHA256-based integrity check
├── status                          # the formatted topology table
├── upgrade      --target-version X.Y.Z --strategy rolling
├── verify       --full             # the 5-check Phase-5 battery
└── version
```

---

## Quick start (macOS / Linux / WSL2)

```bash
# 0. Prereqs: Podman 4+ (preferred) or Docker, Ansible 2.14+, Go 1.22+
./redis-tool doctor

# 1. Build image, start six SSH-able containers
./redis-tool infra up

# 2. Install Redis 7.0.15 on all six and form a 3+3 cluster
./redis-tool provision --version 7.0.15

# 3. Insert 1000 deterministic keys, verify
./redis-tool data seed --keys 1000
./redis-tool data verify

# 4. Topology
./redis-tool status

# 5. Rolling upgrade to 7.2.6
./redis-tool upgrade --target-version 7.2.6 --strategy rolling

# 6. Full health check
./redis-tool verify --full
```

`./redis-tool` is a thin Bash wrapper that builds the Go binary into `bin/`
on first use (or after any `.go` file changes) and re-execs it. So the first
command takes a few extra seconds; every command after that is instant.

---

## Prerequisites

| Tool             | Min version | Notes                                                   |
| ---------------- | ----------- | ------------------------------------------------------- |
| Podman           | 4.0+        | **Preferred** - fully open source (Apache 2.0).         |
| Docker (alt.)    | 20.10+      | Either works; the tool autodetects and prefers Podman.  |
| Compose          | v2.x        | `podman compose` / `docker compose` (plugin or legacy). |
| Ansible          | 2.14+       | `ansible-playbook` must be on `$PATH`.                  |
| Go               | 1.22+       | Only needed to build the CLI binary.                    |
| `bash`, `ssh`    | any         | The Bash wrapper and `scripts/generate-keys.sh`.        |

`./redis-tool doctor` checks everything in one go and prints actionable install
hints if anything's missing.

### Windows note

Ansible cannot run as a control node on native Windows.  Use **WSL2 Ubuntu**
(or Linux/macOS). Docker Desktop with the WSL2 backend works; run all
`./redis-tool` commands from inside the Ubuntu shell.

---

## Architecture at a glance

```
                ┌────────── control node (your laptop / WSL2) ───────────┐
                │                                                        │
                │  ./redis-tool ──► Go orchestrator                      │
                │       │                                                │
                │       └──► ansible-playbook ──► SSH on 127.0.0.1:221X  │
                │                                                        │
                └──────────────────────┬─────────────────────────────────┘
                                       │ SSH
        ┌──────────────────────────────┴──────────────────────────────┐
        │                                                             │
   ┌────▼────────┐  ┌────────────┐  ┌────────────┐  ...  ┌──────────┐ │
   │ redis-node-1│  │redis-node-2│  │redis-node-3│       │redis-…-6 │ │
   │ 10.10.0.11  │◄─┤ 10.10.0.12 │◄─┤ 10.10.0.13 │       │10.10.0.16│ │   redis cluster bus
   │ :6379       │  │ :6379      │  │ :6379      │       │ :6379    │ │   (on 10.10.0.0/24)
   └─────────────┘  └────────────┘  └────────────┘       └──────────┘ │
        Docker / Podman network `redis_net` (10.10.0.0/24) ───────────┘
```

### Two networks doing two different jobs

The single design choice that makes this portable across Linux, Docker Desktop,
Podman (rootful and rootless), and macOS is keeping host↔node traffic and
node↔node traffic on **different network paths**:

| Traffic                          | Path                                  | Why                                                                                                       |
| -------------------------------- | ------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Ansible → managed nodes (SSH)    | `127.0.0.1:221X` (published ports)    | Docker Desktop and rootless Podman do **not** route host→container-IP. Published ports always work.       |
| Redis bus, replication, announce | `10.10.0.1X:6379` (container network) | Cluster needs a stable per-node address that other nodes can reach.                                       |

The host never speaks the Redis protocol. **Every** Redis operation
(`CLUSTER CREATE`, `SET`, `GET`, `CLUSTER FAILOVER`, `INFO`) runs *inside* a
container via Ansible, so host-to-container-IP routing never matters.

### Who does what

- **Go owns orchestration + intelligence.** State machine for the rolling
  upgrade, health gates, progress lines, error handling, structured logging,
  prereq check, runtime detection.
- **Ansible owns mechanical per-node execution.** Each playbook is a "dumb
  verb": install version X, render redis.conf, stop Redis here, run this
  redis-cli on the seed node, etc.
- **Structured handoff.** Playbooks that produce data (status, seed, verify)
  consolidate it on the seed node and `delegate_to: localhost` to write
  `.run/{status,seed,verify}.json` on the control node. Go reads those -
  no regex-on-stdout fragility.

---

## How each command works

### `doctor`

Detects `podman` (preferred) then `docker`, parses `ansible-playbook
--version`, asserts ≥ 2.14. Exits non-zero with install instructions if
anything's missing. **Runs as the first thing on every other command too.**

### `infra up | down | status`

Thin wrapper over `podman compose` / `docker compose -f infra/compose.yml`.
On first `up` it also generates an Ed25519 keypair under `~/.ssh/` (private)
and `infra/keys/` (public) - the public half is baked into the image as
`/root/.ssh/authorized_keys`. Idempotent: a second `infra up` is a no-op.

### `provision --version X.Y.Z`

1. `playbooks/preflight.yml` - SSH ping every node.
2. `playbooks/provision.yml` - installs the **exact** requested version from
   source (the only way to honor `--version X.Y.Z`, since apt doesn't carry
   arbitrary point releases). Idempotent: if the binary on disk is already
   the right version, the build is skipped.
3. Renders `redis.conf` (cluster mode, AOF on, `cluster-announce-ip` = the
   node's 10.10.0.1X address) and starts Redis as a daemon.
4. Runs `redis-cli --cluster create ... --cluster-replicas 1 --cluster-yes`
   on the seed node - that gives us exactly 3 masters + 3 replicas with the
   hash slots auto-assigned. Skips this step if the cluster is already formed.
5. Calls `status` so you see the topology immediately.

### `data seed --keys N` / `data verify`

Deterministic scheme: `value = sha256_hex("key:NNNN")`. `seed` inserts via
`redis-cli -c` (cluster-aware, follows MOVED redirects) so the 1000 keys
distribute across all three masters by hash slot. `verify` recomputes the
hash for each key and counts `verified`, `missing`, `mismatched` separately -
so a failure message says *what* broke, not just *that* something broke.

### `status`

Runs `playbooks/gather_status.yml`, which:

- runs `INFO server`, `INFO memory`, `INFO replication`, `DBSIZE` on every
  node, and
- runs `CLUSTER INFO` + `CLUSTER NODES` on the seed,
- delegates a final task to `localhost` to write `.run/status.json` with the
  fully merged state.

Go reads that JSON and renders the table:

```
Cluster State: ok

MASTERS
10.10.0.11:6379  [master]  v7.0.15  slots: 0-5460       keys: 332  mem: 2.1M
10.10.0.12:6379  [master]  v7.0.15  slots: 5461-10922   keys: 341  mem: 2.0M
10.10.0.13:6379  [master]  v7.0.15  slots: 10923-16383  keys: 327  mem: 1.9M

REPLICAS
10.10.0.14:6379  [replica] v7.0.15  replicating: 10.10.0.11:6379  mem: 2.1M
10.10.0.15:6379  [replica] v7.0.15  replicating: 10.10.0.12:6379  mem: 2.0M
10.10.0.16:6379  [replica] v7.0.15  replicating: 10.10.0.13:6379  mem: 1.9M
```

### `upgrade --target-version X.Y.Z --strategy rolling`

The strategy below keeps `cluster_state:ok` throughout the entire upgrade and never disconnects
a client from a master - only ever from a node that is currently a replica.

#### Why **replicas first, then masters via failover**?

A Redis Cluster client only sends writes to masters. If we take down a
master, every write that hashes into that master's slot range fails until a
replica takes over (~5s with default `cluster-node-timeout`). That's
client-visible downtime. The way around it:

1. **Phase 1 - upgrade the 3 replicas, one at a time.**  
   A replica going offline causes **no client impact** at all (clients only
   talk to masters). For each replica: `SHUTDOWN NOSAVE` → install target
   → start → wait for `master_link_status:up` *and* `cluster_state:ok` →
   move on. Health-gate between every step.

2. **Phase 2 - upgrade the 3 masters, one at a time, via failover.**  
   For each still-old master M, find its (already upgraded) replica R, then:
   - `CLUSTER FAILOVER` on R. Redis runs a synchronous handshake: R catches
     up to M's last write, then atomically swaps roles. M is now a replica,
     R is the new master, and clients automatically retarget thanks to
     `-MOVED` redirects. No writes are lost.
   - Now M is "just" a replica - taking it down has zero client impact.
     Same upgrade sequence as Phase 1.

At the end, the three original replicas are masters and the three original
masters are replicas. That's expected and fine: `verify --full` asserts slot
coverage and "every master has at least one replica," not specific role
identity.

#### Health gating

Go's orchestrator (`internal/upgrade/state.go`) gates every step on a
refreshed `CLUSTER INFO` from the seed node. If `cluster_state:ok` doesn't
come back within 90s, the upgrade stops with a clear error naming the node
and the step.

#### Idempotency

If every node already reports the target version, `upgrade` exits cleanly
with `All nodes already on vX.Y.Z - nothing to do.` Per-step skipping also
applies - a node already on the target version is dropped from the plan.

### `verify --full`

The five checks from Phase 5:

| Check                | Method                                                                  |
| -------------------- | ----------------------------------------------------------------------- |
| Data integrity       | Re-runs `data verify` (1000/1000 keys).                                 |
| Version consistency  | `INFO server` on every node; expects one distinct `redis_version`.       |
| Slot coverage        | Sums slot ranges from `CLUSTER NODES`; expects exactly 16384.            |
| Master/replica pairs | Asserts every master has at least one replica.                          |
| Replication links    | `INFO replication` on every replica; expects `master_link_status:up`.   |

Each check prints `PASS`/`FAIL` with a one-line detail; non-zero exit on any FAIL.

---

## Repository layout

```
.
├── README.md                        # this file
├── redis-tool                       # bash wrapper -> bin/redis-tool
├── redis-tool.yaml                  # default config (override via flags / env)
├── go.mod / main.go
├── Makefile                         # convenience targets + `make demo`
├── cmd/                             # Cobra commands (one file per verb)
│   ├── root.go      doctor.go       version.go
│   ├── infra.go     provision.go
│   ├── data.go      status.go
│   └── upgrade.go   verify.go
├── internal/
│   ├── prereq/       # runtime + ansible detection / version gating
│   ├── runtime/      # podman vs docker abstraction
│   ├── runner/       # ansible-playbook invocation + .run/*.json round-tripping
│   ├── model/        # ClusterStatus / Node / parse CLUSTER NODES
│   ├── render/       # the tabwriter-aligned status table
│   ├── config/       # viper-backed config loader
│   ├── logging/      # structured slog JSON to logs/*.jsonl
│   └── upgrade/      # the rolling-upgrade state machine
├── ansible/
│   ├── ansible.cfg
│   ├── inventory/hosts.ini          # 127.0.0.1:221X for SSH, 10.10.0.1X for Redis
│   ├── group_vars/all.yml
│   ├── roles/redis/
│   │   ├── defaults/main.yml
│   │   ├── tasks/{main,install,configure}.yml
│   │   ├── templates/redis.conf.j2
│   │   └── handlers/main.yml
│   └── playbooks/
│       ├── preflight.yml            # ping + python check
│       ├── provision.yml            # role + start + cluster-create
│       ├── gather_status.yml        # writes .run/status.json
│       ├── data_seed.yml            # writes .run/seed.json
│       ├── data_verify.yml          # writes .run/verify.json
│       ├── upgrade_node.yml         # single-node atomic upgrade
│       └── failover.yml             # CLUSTER FAILOVER on a named replica
├── infra/
│   ├── Containerfile                # Ubuntu 22.04 + sshd + build toolchain
│   ├── compose.yml                  # 6 services, static IPs, port-published SSH
│   └── keys/                        # public half of the SSH keypair
├── scripts/generate-keys.sh         # idempotent ed25519 keygen
├── logs/                            # structured per-run JSON logs (slog)
├── output/                          # captured terminal transcripts for submission
└── .run/                            # gitignored: ansible→Go JSON drop point
```

---

## Trade-offs and known limitations

These are deliberate; I'd revisit each one for a production tool.

- **Root SSH user in the containers.** Simpler than a sudoer setup for a
  lab. In production I'd create a dedicated `ansible` user with NOPASSWD
  sudo on a narrow allowlist and rotate the keypair. The current setup uses
  `PermitRootLogin prohibit-password` (keys only - passwords disabled).

- **Source build vs apt packages.** Apt doesn't carry arbitrary `X.Y.Z`
  point releases.Building from source takes 1–2 min/node but runs in parallel across all
  six. A faster alternative is to build once on the control node and
  distribute the binary - a clean optimization but adds a layer of caching
  logic that isn't necessary to demonstrate the rest of the design.

- **Single seed node for cluster-wide ops.** `redis-node-1` is the
  delegation target for `CLUSTER CREATE`, seed, verify, and the
  `gather_status` consolidator. It's a single point of failure for those
  operations. In a production rewrite the seed selection would float to the
  first reachable master discovered at runtime.

- **No automatic rollback.** Per the spec, the tool stops on the first
  failed step and leaves the cluster as-is. Adding `rollback
  --target-version X.Y.Z` is a straightforward extension (the
  `upgrade_node.yml` playbook is symmetric in either direction).

- **No TLS / no AUTH.** Lab cluster. `protected-mode no` + `bind 0.0.0.0`
  inside the container network. Both would need turning on for anything
  production-shaped, and a `requirepass` / `masterauth` rotation flow.

- **CLUSTER FAILOVER without `FORCE` / `TAKEOVER`.** We do the
  synchronous variant (no flag), which waits for the replica to fully catch
  up before swapping roles - that's what gives us zero data loss. The cost
  is that we can't fail over if the master is unreachable at the protocol
  level. For an emergency upgrade where the master is hung, `FAILOVER FORCE`
  would be the right escape hatch and would be worth flagging.

- **Idempotency is partial.**  
  - `provision` re-run on a healthy cluster: no-op (verified - install task
    skips, cluster-create branch skips).  
  - `upgrade` when target == current: no-op (verified - plan is empty).  
  - `data seed` re-run: overwrites with the same deterministic values, so
    `verify` still passes. There's no "are we already seeded" short circuit.

- **Health-gate timeout is 90s per step.** Generous for a local lab,
  potentially too tight for a slow network. The constant lives in
  `cmd/upgrade.go` and would graduate to a flag in a real tool.

---

## Stretch goals implemented

- **S4 - Idempotency.** Re-running `provision` is a no-op (install task
  skipped when binary version matches; cluster-create branch skipped when
  `cluster_state:ok`). Re-running `upgrade` when all nodes already report
  the target version exits cleanly without contacting Ansible's per-step
  playbooks.
- **S5 - Structured logging.** Every command writes `logs/<UTC-stamp>-<cmd>.jsonl`
  via Go's `slog` JSON handler, with `action`/`status`/`node` keys per step.
  The log path is printed on exit.
- `--dry-run` prints the `ansible-playbook` invocations without running
  them - useful for demos and for understanding what the tool is about to
  do.

Not implemented (would be next):

- **S1/S2 scale-out/scale-in** - `redis-cli --cluster add-node` / `reshard`
  flows. Mechanical extensions to the same orchestration pattern.
- **S3 rollback** - runs the same `upgrade_node.yml` with the old version
  and the same role-swap dance. ~50 lines of Go to add.

---

## Running everything in one shot

```bash
make demo   # builds, brings up infra, provisions, seeds, status,
            # rolling-upgrades 7.0.15 -> 7.2.6, full verify
            # Output files land in output/*.txt for submission.
```
