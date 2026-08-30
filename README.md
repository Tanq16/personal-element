<div align="center">
  <h1>element</h1>

  <a href="https://github.com/Tanq16/personal-element/actions/workflows/release.yaml"><img alt="Build Workflow" src="https://github.com/Tanq16/personal-element/actions/workflows/release.yaml/badge.svg"></a>&nbsp;<a href="https://github.com/Tanq16/personal-element/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/Tanq16/personal-element"></a><br><br>
  <a href="#features">Features</a> &bull; <a href="#install">Install</a> &bull; <a href="#usage">Usage</a> &bull; <a href="#notes">Notes</a>
</div>

---

A private Matrix homeserver on a small ARM VPS, serving text channels and Discord-style drop-in voice to a small group of friends.

It exists so the group owns its own messages and calls. It is not federated, it does not accept registrations, and it is not intended to grow past roughly ten people.

Everything needed to rebuild it is in this repository, in `docs/`, numbered in the order they are worth reading.

| | Covers |
|---|---|
| [01-deployment.md](docs/01-deployment.md) | every configuration file and the order to apply them in |
| [02-operations.md](docs/02-operations.md) | accounts, rooms, backup, upgrades, and the runbooks |
| [03-clients.md](docs/03-clients.md) | what each person does on their own device |
| [04-element-agent.md](docs/04-element-agent.md) | the agent application service |
| [05-decisions.md](docs/05-decisions.md) | why this stack, and why each load-bearing setting is what it is |
| [99-future-claude-code-channels.md](docs/99-future-claude-code-channels.md) | a session-based agent design that was not built |

## Features

- **Text channels grouped into a space**, with per-channel access rules. Everyone with an account joins freely; individual channels can be invite-only.
- **Voice and video** through a self-hosted LiveKit SFU. No third-party SFU, no TURN relay, no external call service.
- **AI agents as first-class members.** An agent is a real Matrix account created by the `element-agent` application service and mentioned by name like anyone else. What it does when mentioned runs on the machine of whoever reserved it.
- **Closed by construction.** Federation is off at the listener, registration is off, and the admin API is bound to loopback. An account created by the admin is the only way in.
- **Plain-text history.** Channels are unencrypted, so `pg_dump` and a media tarball are a complete, readable backup.

## Install

### Host

| | |
|---|---|
| Machine | 2 ARM cores, 12 GB RAM, 200 GB disk |
| Network | 2.0 Gbps |
| OS | Ubuntu 24.04 aarch64 |
| Public IP | `203.0.113.10`, ephemeral |

```sh
ssh -i .env/ssh_element ubuntu@203.0.113.10
```

`.env/` holds the provider credentials, the instance SSH key, and `deployment.env` carrying this deployment's real hostnames, addresses and account names. It is ignored by git, and every such value in this repository is a placeholder.

### The stack

Six containers from `~/element/compose.yaml`, started with `docker compose up -d`. Every service carries `restart: unless-stopped`, so Docker brings them back after a reboot without a systemd unit.

| Service | Image | Bind |
|---|---|---|
| `synapse` | `ghcr.io/element-hq/synapse:v1.159.0` | `127.0.0.1:8008` |
| `postgres` | `postgres:17.9-alpine` | compose network only |
| `livekit` | `livekit/livekit-server:v1.13.6` | host network |
| `lk-jwt` | `ghcr.io/element-hq/lk-jwt-service:0.6.0` | `127.0.0.1:8080` |
| `element-web` | `vectorim/element-web:v1.12.26` | `127.0.0.1:8081` |
| `element-call` | `ghcr.io/element-hq/element-call:v0.24.0` | `127.0.0.1:8082` |

Idle footprint is about 240 MB total. Only Caddy binds a public port; everything else is loopback or, for LiveKit, closed at the firewall.

Caddy fronts all of it on 443 and is the one component not under Docker's restart policy, because it runs from a tmux session alongside the web terminal and code-server. Four ports are open inbound: 22, 443, 7881/tcp, and 50000-60000/udp. Port 80 is closed, and certificates are issued over TLS-ALPN-01.

The full build, from DNS and firewall through every configuration file to the verification suite, is in [docs/01-deployment.md](docs/01-deployment.md). Two steps in it are ordered rather than free: Synapse's signing key has to be generated before the first `up`, and Postgres has to be initialised with `C` collation before it holds any data.

### element-agent

Built in `element-agent/` in this repository and not deployed yet. One Go binary in two modes. The server runs beside Synapse as an application service and turns an `@agent_*` mention into a job. The client runs on a person's own machine and executes that job as a one-shot command, so nothing about an agent's behavior lives on the server.

The client holds no Matrix credential. Every read and write is the server's, using the `as_token` with `?user_id=` to act as the agent.

The four secrets, the `config.yaml` keys, the Synapse registration file, the compose service, the Caddy route, and the deployment order are in [docs/04-element-agent.md](docs/04-element-agent.md).

## Usage

The API commands referenced below run on the host. The admin API is loopback-only, so they work from the web terminal and from nowhere else. Accounts, room deletion, access rules, backup, upgrades, and the ghost-call runbook are all in [docs/02-operations.md](docs/02-operations.md).

### Accounts

Registration is off, so accounts are made on the host.

```sh
cd ~/element
docker compose exec synapse register_new_matrix_user \
  -c /data/homeserver.yaml --no-admin http://localhost:8008
```

`--no-admin` is the default to reach for. Server admin is a flag on the user row granting the whole `/_synapse/admin` API, and it is unrelated to power levels inside any room. Only `@admin:element.example.com` holds it.

### AI agents

An agent is a Matrix account in the `@agent_*` namespace, created and driven by the `element-agent` application service. It answers only when a message carries an explicit mention of it, and it answers on the machine of whoever reserved it.

Each person sets their own machine up once, then reserves as many agents as they want:

```sh
element-agent client setup --server-url https://element.example.com/<prefix>
element-agent client init reviewer
element-agent client register reviewer --allow-message-retrieval -- \
  claude -p '{{prompt}}' --dangerously-skip-permissions --model haiku
element-agent client serve
```

`init` claims the name and scaffolds `~/.config/element-agent/agents/reviewer/`, holding an `AGENTS.md` and a `.agents/skills/` for its owner to fill, with `CLAUDE.md` and `.claude/skills` symlinked onto them. A reserved agent is joined to every room and answers nothing. `register` records the command and turns it on, `deregister` turns it back off without deleting anything, and `deregister --release` gives the name up.

The name is held by a claim token minted at `init` and stored in the agent's directory, so a name reserved from one machine cannot be registered, released, or served from another.

The prompt carries the mentioning message and nothing else, and the agent answers with whatever it writes to `.result` in its own directory. `--allow-message-retrieval` adds a paragraph letting it walk backwards through the room ten messages at a time.

There is no per-agent room scoping. Every agent is joined to every space and every channel, and an agent only ever answers an explicit mention. Direct messages are excluded structurally rather than by a filter, because the reconciler walks spaces and their children and a DM is not a space child.

A bot outside the appservice is still an ordinary account holding an ordinary token, which [docs/02-operations.md](docs/02-operations.md#bots-outside-the-appservice) covers.

### Clients

Element Web at `https://element-chat.example.com` needs no configuration. Element X and Element Desktop both bundle their own copy of Element Call carrying no SFU address, so each device needs a developer-options override pointing at `https://element-call.example.com/room`, with the `/room` suffix mandatory. `app.element.io` cannot be used at all, because calls there fall back to a legacy path needing a TURN server this deployment does not have.

That, and device verification and recovery, are in [docs/03-clients.md](docs/03-clients.md).

## Notes

- **The public IP is ephemeral.** Stopping and starting the instance preserves it, and terminating it does not. Moving to a static address means a new address and a DNS change.

- **Capacity.** Against LiveKit's published benchmarks on a 16-core machine, a ten-person voice channel costs roughly a quarter of one core here, and ten-way video roughly one of the two. Bandwidth is never the constraint: ten-way video is about 56 Mbps against 2 Gbps.

- **There is nowhere on the host to keep a backup.** The whole disk is the boot volume, so every backup has to leave the machine.

- **Synapse has no built-in web admin.** Administration is the HTTP API plus `register_new_matrix_user`. [Ketesa](https://github.com/etkecc/ketesa) is the web UI over that API and ships a prebuilt `/admin` subpath build; it was deliberately skipped because the web terminal covers the same ground for a group this size.
