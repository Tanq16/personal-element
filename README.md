<div align="center">
  <h1>element</h1>

  <a href="https://github.com/Tanq16/personal-element/actions/workflows/release.yaml"><img alt="Build Workflow" src="https://github.com/Tanq16/personal-element/actions/workflows/release.yaml/badge.svg"></a>&nbsp;<a href="https://github.com/Tanq16/personal-element/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/Tanq16/personal-element"></a><br><br>
  <a href="#features">Features</a> &bull; <a href="#install">Install</a> &bull; <a href="#usage">Usage</a> &bull; <a href="#decisions">Decisions</a> &bull; <a href="#notes">Notes</a>
</div>

---

A private Matrix homeserver on a small ARM VPS, serving text channels and Discord-style drop-in voice to a small group of friends.

It exists so the group owns its own messages and calls. It is not federated, it does not accept registrations, and it is not intended to grow past roughly ten people.

Everything needed to rebuild it is in this repository. [docs/deployment.md](docs/deployment.md) holds every configuration file and the order to apply them in, [docs/operations.md](docs/operations.md) covers running it, [docs/clients.md](docs/clients.md) covers what each person does on their own device, and [docs/element-agent.md](docs/element-agent.md) covers the agent application service.

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

Idle footprint is about 240 MB total. Only Caddy binds a public port; everything else is loopback or, for LiveKit, closed at the security list.

Caddy fronts all of it on 443 and is the one component not under Docker's restart policy, because it runs from a tmux session alongside the web terminal and code-server. Four ports are open inbound: 22, 443, 7881/tcp, and 50000-60000/udp. Port 80 is closed, and certificates are issued over TLS-ALPN-01.

The full build, from DNS and firewall through every configuration file to the verification suite, is in [docs/deployment.md](docs/deployment.md). Two steps in it are ordered rather than free: Synapse's signing key has to be generated before the first `up`, and Postgres has to be initialised with `C` collation before it holds any data.

### element-agent

Built in `element-agent/` in this repository and not deployed yet. One Go binary in two modes. The server runs beside Synapse as an application service and turns an `@agent_*` mention into a job. The client runs on a person's own machine and executes that job as a one-shot command, so nothing about an agent's behavior lives on the server.

```
Element client
   │  "@agent_reviewer is that number right?"
   ▼
Synapse ── PUT /_matrix/app/v1/transactions/{txnId} ──▶ element-agent server (container)
   ▲                                                          │  match m.mentions.user_ids
   │                                                          │  no match → drop
   │                                                          │  match → compose the job
   │                                                          ▼
   │                                    wss://element.example.com/<prefix>/ws
   │                                                          ▼
   │                                    element-agent client (laptop, `client serve`)
   │                                                          │  exec argv in the agent's directory
   │                                                          ▼
   └── PUT /rooms/{roomId}/send/m.room.message?user_id=@agent_reviewer:element.example.com
```

The client holds no Matrix credential. Every read and write is the server's, using the `as_token` with `?user_id=` to act as the agent.

The four secrets, the `config.yaml` keys, the Synapse registration file, the compose service, the Caddy route, and the deployment order are in [docs/element-agent.md](docs/element-agent.md).

## Usage

The API commands referenced below run on the host. The admin API is loopback-only, so they work from the web terminal and from nowhere else. Accounts, room deletion, access rules, backup, upgrades, and the ghost-call runbook are all in [docs/operations.md](docs/operations.md).

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

A bot outside the appservice is still an ordinary account holding an ordinary token, which [docs/operations.md](docs/operations.md#bots-outside-the-appservice) covers.

### Clients

Element Web at `https://element-chat.example.com` needs no configuration. Element X and Element Desktop both bundle their own copy of Element Call carrying no SFU address, so each device needs a developer-options override pointing at `https://element-call.example.com/room`, with the `/room` suffix mandatory. `app.element.io` cannot be used at all, because calls there fall back to a legacy path needing a TURN server this deployment does not have.

That, and device verification and recovery, are in [docs/clients.md](docs/clients.md).

## Decisions

### Matrix and Synapse, over the alternatives

The requirement was Discord-shaped: persistent voice channels, self-hosted, open source, AI agents with real identities, and easy data extraction. On an ARM box, architecture eliminated two candidates outright.

| | Verdict |
|---|---|
| Mattermost | Out. `mattermost-team-edition` publishes `linux/amd64` only. |
| Zulip | Out. `zulip/docker-zulip` publishes `linux/amd64` only, and it has no native voice. |
| Rocket.Chat | Rejected. Community Edition offers video only through Jitsi and BigBlueButton, so no drop-in voice channels. Its own docs ask 4 GiB for the app plus 4 GiB for MongoDB, and MongoDB must run as a replica set even single-node. |
| Stoat (ex-Revolt) | Runner-up. Genuinely Discord-shaped and arm64-native, but its own self-hosting README says most official mobile clients do not work against self-hosted instances, and the stack is 14 containers including RabbitMQ and MinIO. |
| Buzz (Block) | Deferred. The best agent design of the group and a single signed event log is the ideal backup format, but it has no voice at all and the repository was created in March 2026 and says it is unfinished. |

Matrix was the only option clearing every requirement at once, and the LiveKit SFU it needs for voice is the same component Stoat would have required.

### Docker Compose, over native packages

Six components with published arm64 images, upgrades as tag bumps, `docker compose restart <service>` for per-service control, and `POSTGRES_INITDB_ARGS` handling the collation requirement that is painful to get right by hand. Element Server Suite was rejected because it is Kubernetes-only. `matrix-docker-ansible-deploy` was rejected because it takes ownership of Traefik on 80/443, which collides with the Caddy already fronting the web terminal and code-server.

### `server_name: element.example.com`

Permanent, and it is why user IDs read `@name:element.example.com`. Getting `@name:example.com` would have required an apex A record serving two well-known documents. One subdomain was preferred over shorter user IDs.

### Federation off

Changed after the fact, once it became clear that "public" on a federated server means public to the whole Matrix network. The listener resource list went from `[client, federation]` to `[client, openid]`, plus `federation_domain_whitelist: []`.

`openid` has to be named explicitly. Synapse's `federation` resource implies `media`, `keys` and `openid`, so removing it silently removes the OpenID endpoint that lk-jwt-service uses to authorise callers into a call. Dropping that breaks voice with a misleading error.

With federation off, `public` join rules can only ever mean "anyone holding an account created here", so the boundary is structural rather than a checkbox to remember per room.

### Registration off, rooms unencrypted

`enable_registration` and `allow_guest_access` are both false, verified live against `POST /_matrix/client/v3/register`. `encryption_enabled_by_default_for_room_type` is deliberately absent because its default is already `"off"`, and writing `off` in YAML would hand Synapse a boolean where it expects a string. Unencrypted channels are what make bots simple and backups readable. Element still encrypts DMs client-side.

### Self-hosted Element Web and Element Call

Originally judged unnecessary, on the grounds that `app.element.io` is static JavaScript talking directly to this homeserver. That was wrong for calls, for two compounding reasons.

**Legacy calls.** Element offers a legacy peer-to-peer call in any room with two or fewer members, and legacy calls need a TURN server, which this deployment has none. `app.element.io` sets neither `element_call.use_exclusively` nor `setting_defaults.enableLegacyCallsVoip`, and neither can be changed on someone else's deployment.

**Transport discovery.** Element Call resolves its SFU in two steps: the homeserver's `/_matrix/client/unstable/org.matrix.msc4143/rtc/transports` endpoint, then its own `config.json`. Synapse answers step one from `matrix_rtc.transports` in `homeserver.yaml`, which is only registered when `experimental_features.msc4143_enabled` is on. A widget holds no access token and cannot make that authenticated request itself, so matrix-js-sdk asks the hosting client over the widget API, which needs `matrix-widget-api` 1.18.0 or newer at both ends. Step two reads `/widgets/element-call/config.json`, which in the shipped image contains only `matrix_rtc_session` timings and no `livekit` key. Replacing that file is possible only by hosting Element Web.

Element X never reaches that endpoint at all. It renders a bundled Element Call, from `appassets.androidplatform.net` on Android and `EmbeddedElementCall.appURL` on iOS, and that bundle carries no `livekit` key. Across twelve hours of logs, no Element X session requested the transports endpoint, wrote an `org.matrix.msc3401.call.member` event, or asked lk-jwt for a token, while the browser sessions did all three. `element-call.example.com` exists so each phone can be pointed at a deployment that does carry the key.

### LiveKit on host networking

Publishing `50000-60000/udp` through Docker means ten thousand port mappings and a `docker-proxy` for each. LiveKit's own docs say host networking should be used in Docker. The side effect is that LiveKit's API binds `0.0.0.0:7880`, and the security list is the only thing closing it, which is why 7880 must never be added there.

`use_external_ip: true` is required. The VM's interface only carries `10.0.1.31`; the public address is 1:1 NAT, and LiveKit discovers it over STUN at startup.

### SFU webhooks

LiveKit posts room and participant events to lk-jwt at `http://127.0.0.1:8080/sfu_webhook`, signed with the API key from `livekit.yaml`. LiveKit runs on host networking, so that loopback address reaches the published lk-jwt port directly rather than going back out through Caddy.

Without the webhook block, lk-jwt never learns that a participant disconnected, and a browser tab closed uncleanly leaves behind a call membership that nothing clears. Every client in the room then shows a permanent ongoing call that the phones refuse to join. The full mechanism and the two queries that diagnose it are in [docs/operations.md](docs/operations.md#a-call-that-never-ends).

`LIVEKIT_SANITY_CHECK_INTERVAL_SECONDS: "60"` on the lk-jwt service is the pull-based fallback for a webhook that goes missing anyway. It defaults to `0`, which disables it.

`room: auto_create: false` belongs with the same change. `LIVEKIT_FULL_ACCESS_HOMESERVERS` decides who lk-jwt will create a room for, but LiveKit creates one for any valid join token unless auto-create is off, so without it that restriction does nothing.

### element-agent as an application service, not a bot with a token

A Synapse access token is opaque, unscoped and long-lived, and `GET /_matrix/client/v3/joined_rooms` enumerates every room its holder is in. An agent's input is attacker-controlled by construction, since any room member can mention it, so no token goes anywhere near one. Identity is the `as_token` plus a `?user_id=` query parameter on ordinary client-server calls, held only by the server.

Splitting the binary the other way, with behavior on the server, was rejected for the same reason it would have been convenient: it would put everyone's prompts, skills and subscription on one box. The server owns identity, room membership and message plumbing; the person who reserves an agent owns what it runs.

Three things about the split are worth stating, because none of them is the obvious reading:

- History paging reads `GET /rooms/{roomId}/context/{eventId}` for its first page, not `/messages` with a `prev_batch` token. An application service transaction carries no such token, and `/context` returns exactly the events before a given event plus a `start` token that `/messages` then pages from. That first call is made when an agent asks for history rather than when it is dispatched, so an agent that answers from the message alone costs no history call at all.
- The prompt is composed on the client, because whether an agent may read the conversation before its mention is a property of that agent's registration and lives on the machine that owns it.
- `client register` takes the agent command as positional arguments after `--` rather than inside a `--command` flag, because a multi-word argv in a single flag needs a shell-quoting parser.

### No resource limits, no swap

Containers see all 12 GB and both cores by default; adding limits could only cap them below the hardware. The one thing worth setting was log rotation, handled by journald retention rather than the unbounded `json-file` default.

## Notes

- **`server_name` is permanent.** It is embedded in every user ID, room ID, and event on the server, and it has to be decided before the first `generate`.

- **`openid` must be named explicitly** in the listener resources once `federation` is removed, or all voice breaks with an error pointing nowhere near federation.

- **Postgres collation must be `C`** before the data directory is first initialised, or Synapse will not start and fixing it means a dump and restore.

- **`UID`/`GID` in the compose file must match the host user.** They are 1001 here, not the 1000 most guides assume.

- **7880 must never be opened** in the security list while LiveKit runs on host networking.

- **Caddy will not parse a one-line `handle_path` block.** The braces need their own lines.

- **`admin off` in the Caddyfile disables `caddy reload`.** Adding a route means restarting Caddy, which is the one component with no restart policy and no systemd unit, so it comes back only by hand in tmux.

- **`127.0.0.1` in the appservice registration file would not work.** Inside the synapse container that address is Synapse itself, so the appservice `url` is `http://element-agent:9000` over the compose network, `homeserver` is `http://synapse:8008` rather than the loopback default, and `listen` is `0.0.0.0:9000` rather than the loopback default that `config.example.yaml` still shows. Caddy reaches the same process through the published `127.0.0.1:9000` on the host.

- **Releasing an agent name frees it but not the account.** Another machine reserving it gets a new claim token and reattaches to the same `@agent_<name>` account with its existing history and room memberships. Nothing here deactivates an account, and Synapse would not release the localpart even if it did.

- **A deregistered or released agent does not leave any room.** It stays joined to the space and every channel and stays in every member list. It goes quiet, it does not disappear.

- **Agents run with `--dangerously-skip-permissions`.** Whoever reserves one accepts that it acts with their own machine access on text written by anyone in the room, and guardrails are theirs to write into its instructions file. Allow rules have no effect in that mode; deny rules still block, and are the only lever available.

- **The transaction deduplication cache is in memory.** A restart of `element-agent` can therefore cost at most one duplicate reply, if Synapse retries a transaction that was already processed.

- **Every agent joining every channel is visible.** Each one puts a membership event in each timeline and a row in each member list.

- **The public IP is ephemeral.** Stopping and starting the instance preserves it, and terminating it does not. Moving to a static address means a new address and a DNS change.

- **Element Call cannot be pointed at centrally.** Element Desktop bundles its own copy inside the asar and reads `Developer.elementCallUrl` at `[SettingLevel.DEVICE]`, which no config file can set. Element X bundles its own and reads a per-device override.

- **`/.well-known/element/element.json` is not read by Element X.** The `{"call":{"widget_url":"..."}}` format is documented in [element-meta#2441](https://github.com/element-hq/element-meta/issues/2441), but neither the element-x-android nor the element-x-ios source contains any reference to it. The file is served and harmless. The per-device developer setting is what actually works.

- **The `/room` suffix is mandatory** in the Element X and Element Desktop override, because a bare origin renders Element Call's standalone home page, which needs an access token a widget does not have.

- **Capacity.** Against LiveKit's published benchmarks on a 16-core machine, a ten-person voice channel costs roughly a quarter of one core here, and ten-way video roughly one of the two. Bandwidth is never the constraint: ten-way video is about 56 Mbps against 2 Gbps.

- **There is nowhere on the host to keep a backup.** The whole disk is the boot volume, so every backup has to leave the machine.

- **Synapse has no built-in web admin.** Administration is the HTTP API plus `register_new_matrix_user`. [Ketesa](https://github.com/etkecc/ketesa) is the web UI over that API and ships a prebuilt `/admin` subpath build; it was deliberately skipped because the web terminal covers the same ground for a group this size.

- **Non-admins can create their own spaces.** Synapse has no setting to prevent it; `rc_room_creation` only rate-limits. Blocking it would need the third-party `synapse-user-restrictions` module, from which server admins are exempt anyway.
