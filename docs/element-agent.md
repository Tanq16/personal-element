# element-agent

One Go binary in two modes. The server runs beside Synapse as a Matrix application service and turns an `@agent_*` mention into a job. The client runs on a person's own machine and executes that job as a one-shot command, so nothing about an agent's behavior lives on the server.

It is built in `element-agent/` and is not deployed. None of the files below exist on `203.0.113.10` yet.

## How it works

```
   │  "@agent_reviewer is that number right?"
   ▼
Synapse ── PUT /_matrix/app/v1/transactions/{txnId} ──▶ element-agent server (container)
   ▲                                                          │  job frame over WebSocket
   │                                                          ▼
   │                                    element-agent client (laptop, `client serve`)
   │                                                          │  exec argv in the agent's directory
   │                                                          ▼
   │                                                     result frame
   └── PUT /rooms/{roomId}/send/m.room.message?user_id=@agent_reviewer:element.example.com
```

The client holds no Matrix credential. Every read and write is the server's, using the `as_token` with `?user_id=` to act as the agent. An agent's input is attacker-controlled by construction, since any room member can mention it, so no Matrix token ever reaches the machine that runs the model.

The server exposes six routes, on one port, split by which token opens them.

| Route | Authorization | Caller |
|---|---|---|
| `PUT /_matrix/app/v1/transactions/{txnId}` | `Bearer <hs_token>` | Synapse |
| `GET /_matrix/app/v1/users/{userId}` | `Bearer <hs_token>` | Synapse |
| `POST /<prefix>/reserve` | `Bearer <shared secret>` | client |
| `POST /<prefix>/register` | `Bearer <shared secret>` + claim | client |
| `POST /<prefix>/deregister` | `Bearer <shared secret>` + claim | client |
| `GET /<prefix>/ws` | `Bearer <shared secret>` + claim per agent | client |

## Secrets

Four values in `~/element/.env.agents` at mode 0600, referenced from compose with `env_file:` so they stay out of `docker inspect`. All four are required; the server exits at config load naming the ones that are unset.

| Variable | Value | Used for |
|---|---|---|
| `ELEMENT_AGENT_AS_TOKEN` | invented, `openssl rand -hex 32` | every Matrix call, with `?user_id=` to act as an agent |
| `ELEMENT_AGENT_HS_TOKEN` | invented, `openssl rand -hex 32` | validating `Authorization` on inbound transactions |
| `ELEMENT_AGENT_ADMIN_TOKEN` | an access token for `@admin` | `GET /_synapse/admin/v1/rooms` and `POST /_synapse/admin/v1/join/{room}` only |
| `ELEMENT_AGENT_SHARED_SECRET` | invented, `openssl rand -hex 32` | the three client routes, handed to each person by hand |

`as_token` and `hs_token` must be byte-identical to the values in `agents.yaml`, since Synapse and the server compare them directly.

The admin token comes from one password login rather than being invented, and it can act as any user on the homeserver. The room reconciler is the only thing that reaches it.

```sh
curl -sS -X POST http://127.0.0.1:8008/_matrix/client/v3/login \
  -H 'Content-Type: application/json' \
  -d '{"type":"m.login.password","identifier":{"type":"m.id.user","user":"admin"},"password":"<password>"}'
```

Keep `access_token` from that response. `nonrefreshable_access_token_lifetime` and `session_lifetime` both default to `infinity`, so it does not expire on its own.

The shared secret is the same string for everybody and is the only thing gating agent registration and the WebSocket. Rotating it invalidates every `token.json` on every machine.

```sh
umask 077
cat > ~/element/.env.agents <<'EOF'
ELEMENT_AGENT_AS_TOKEN=...
ELEMENT_AGENT_HS_TOKEN=...
ELEMENT_AGENT_ADMIN_TOKEN=...
ELEMENT_AGENT_SHARED_SECRET=...
EOF
```

## Configuration

`~/element/element-agent/config.yaml`, mounted read-only at `/etc/element-agent/config.yaml`. Omitted keys fall back to the defaults below.

| Key | Default | Description |
|---|---|---|
| `homeserver` | `http://127.0.0.1:8008` | Synapse base URL |
| `server_name` | required | the part after the colon in every agent MXID |
| `listen` | `127.0.0.1:9000` | bind address |
| `client_prefix` | required | the random path prefix clients connect through |
| `state_path` | `/data/state.json` | agent names, statuses, and claim tokens, written at mode 0600 |
| `reconcile_every` | `5m` | how often every agent is reconciled into every space and channel |
| `backfill_limit` | `10` | messages returned per walk-back page |

Two defaults are wrong inside a container and have to be overridden. Inside the Synapse container `127.0.0.1` is Synapse itself, so the server is reached by service name over the compose network, and it has to bind every interface for that to work.

```yaml
homeserver: http://synapse:8008
server_name: element.example.com
listen: 0.0.0.0:9000
client_prefix: /<prefix>
state_path: /data/state.json
reconcile_every: 5m
backfill_limit: 10
```

`client_prefix` is a random path segment, generated once with `openssl rand -hex 16` and used in three places that must agree: this file, the Caddy route, and the `--server-url` every person passes to `client setup`. A path rather than a subdomain, because a subdomain leaks through TLS SNI and is published permanently in Certificate Transparency when Let's Encrypt issues for it.

## Synapse registration

`~/element/data/synapse/agents.yaml`. That directory is already mounted at `/data` in the Synapse container, so the file needs no new bind mount and Synapse can be restarted rather than recreated.

```yaml
id: agents
url: http://element-agent:9000
as_token: <ELEMENT_AGENT_AS_TOKEN>
hs_token: <ELEMENT_AGENT_HS_TOKEN>
sender_localpart: agents
rate_limited: false
namespaces:
  users:
    - exclusive: true
      regex: "@agent_.*:element\\.example\\.com"
  aliases: []
  rooms: []
```

Referenced from `~/element/data/synapse/homeserver.yaml`, which has no such key today:

```yaml
app_service_config_files:
  - /data/agents.yaml
```

`rate_limited: false` is set explicitly, because `rc_message` is `per_second: 0.5, burst_count: 30` and an agent posting a chunked answer would hit it. `exclusive: true` makes Synapse reject with `M_EXCLUSIVE` any attempt by a normal user to register a name in the namespace. The `sender_localpart` user `@agents:element.example.com` is created by Synapse and used by no code path.

## The container

Added to `~/element/compose.yaml`:

```yaml
  element-agent:
    image: element-agent:latest
    build: ./element-agent
    env_file: .env.agents
    volumes:
      - ./element-agent/config.yaml:/etc/element-agent/config.yaml:ro
      - ./data/element-agent:/data
    ports:
      - "127.0.0.1:9000:9000"
    restart: unless-stopped
```

The image runs as uid 10001, so the state directory has to be owned by it or the state file cannot be written.

```sh
mkdir -p ~/element/data/element-agent
sudo chown 10001:10001 ~/element/data/element-agent
```

The published port is for Caddy, which runs on the host rather than in compose. Synapse reaches the same process at `http://element-agent:9000` over the compose network.

## Caddy

One route in the `element.example.com` block, ahead of the catch-all:

```
handle /<prefix>/* {
    reverse_proxy 127.0.0.1:9000
}
```

The Caddyfile sets `admin off`, which also disables the endpoint `caddy reload` talks to, so this route needs Caddy restarted rather than reloaded. Caddy runs from a tmux session with no restart policy, so that restart is manual and drops every connection through it for as long as it takes to come back.

## Deploying

One restart, of Synapse only. `app_service_config_files` is parsed at config load, and the only things Synapse re-reads on `SIGHUP` are the cache factors and the log config, so a running server cannot pick the registration file up.

1. Log in once as `@admin` and keep the access token.
2. Write `.env.agents`, `data/synapse/agents.yaml`, and `element-agent/config.yaml`.
3. `mkdir -p ~/element/data/element-agent && sudo chown 10001:10001 ~/element/data/element-agent`
4. Add the service to `compose.yaml`, then `docker compose up -d --build element-agent`. Naming the service means compose creates that container and touches no other.
5. Add `app_service_config_files` to `data/synapse/homeserver.yaml`.
6. `docker compose restart synapse`. This is the disruption. Clients reconnect on their own and channels are unencrypted, so nothing is lost, but a Synapse restart against a call in progress has not been tested here.
7. Add the Caddy route, then restart Caddy.

Step 4 comes before step 6 so Synapse is not retrying transactions against a closed port on its way up. The other order is harmless: with an empty `state.json` the reconciler returns before it calls Synapse at all. Get `agents.yaml` right before step 6, because Synapse refuses to start on a malformed appservice config and that turns one restart into an outage.

After this there are no further restarts. Reserving an agent creates its account over the appservice API and the reconciler joins it to every room live, and channels created later are picked up by the timer.

## Using it from a machine

Once per machine, then once per agent.

```sh
element-agent client setup --server-url https://element.example.com/<prefix>
element-agent client init reviewer
element-agent client register reviewer --allow-message-retrieval -- \
  claude -p '{{prompt}}' --dangerously-skip-permissions --model haiku
element-agent client serve
```

`setup` prompts for the shared secret rather than taking it inline, so it stays out of shell history. `client serve` holds one WebSocket for every agent on that machine, binds `127.0.0.1:5167`, and must be running for a mention to reach anything. A second instance exits on `EADDRINUSE`.

An agent moves through three states, and only one of them answers a mention.

| Command | State | A mention does |
|---|---|---|
| `client init <name>` | reserved | nothing at all |
| `client register <name> -- <argv>` | serving | runs the command and posts the answer |
| `client deregister <name>` | reserved | nothing at all |
| `client deregister <name> --release` | gone | nothing, and the name is free for anyone to reserve |

`init` claims the name, creates the Matrix account, joins it to every space and channel, and scaffolds the directory. The account sits in every member list silently from that moment, which is what buys the time to write its instructions before it answers anything.

```
~/.config/element-agent/
  token.json                       server URL and shared secret, mode 0600
  agents/
    reviewer/
      agent.json                   claim token, argv, flags, mode 0600
      AGENTS.md                    the instructions, yours to write
      .agents/skills/              the skills, yours to fill
      CLAUDE.md          -> AGENTS.md
      .claude/skills     -> ../.agents/skills
      .result                      the last answer the agent wrote
```

The scaffolding is agent-agnostic. `AGENTS.md` and `.agents/skills/` are the real files, and the `CLAUDE.md` and `.claude/skills` symlinks are what makes `claude -p` read them without a second copy to keep in step.

`init` returns a claim token that is written into `agent.json` and held by the server. Every later `register`, `deregister`, and WebSocket connection presents it, so a name reserved from one machine cannot be registered, released, or served from another. A machine announcing a name whose claim does not match is refused that name and keeps the rest of its agents.

`register` records the command and turns the agent on. Everything after `--` is the argv, and the literal `{{prompt}}` in any argument is replaced with the composed prompt. Nothing is deleted by `deregister`, so changing an agent's behaviour is `deregister`, edit, `register` again, and re-registering is also how `--allow-message-retrieval` is turned on or off.

`init`, `register`, and `deregister` all work whether or not `client serve` is running. A running daemon is told over the loopback and picks the change up without a restart.

Never pass `--bare` to `claude -p`: it needs `ANTHROPIC_API_KEY`, which bills the API instead of the subscription, and it skips the whole per-agent directory.

### What the agent receives

The prompt carries the mentioning message and nothing else. The agent writes its answer to `.result` in its own directory, and standard output is used when that file is absent or empty. The daemon deletes `.result` before every job, so a crashed run cannot have its predecessor's answer posted.

`--allow-message-retrieval` appends one paragraph telling the agent it may read the conversation before the message, in batches of `backfill_limit`, by running:

```sh
curl -sS -H "Authorization: Bearer $ELEMENT_AGENT_TOKEN" http://127.0.0.1:5167/context
```

Each call returns the batch before the last one, and repeating it walks backwards until it reports no earlier messages. Without the flag that paragraph is absent, the agent acts on the single message, and the server makes no history call at all.

`ELEMENT_AGENT_TOKEN` is per job and set in the child's environment rather than substituted into the prompt, so a model that echoes its instructions cannot put it in a room.

## Operating notes

- Every agent is joined to every space and every channel, and answers only an explicit mention. Direct messages are excluded structurally, because the reconciler walks spaces and their children and a DM is not a space child.
- Agent names must match `^[a-z0-9][a-z0-9_-]{0,31}$`, and the MXID is `@agent_<name>:element.example.com`.
- An answer larger than one event is split at 57344 bytes of encoded content, which is the 65536 event limit less an 8192 byte envelope reserve.
- The transaction deduplication cache holds 1024 ids in memory, so a restart can cost at most one duplicate reply if Synapse retries a transaction that was already processed.
- Releasing a name frees it but not the account. Another machine reserving it gets a new claim token and reattaches to the same `@agent_<name>` account with its existing history and room memberships.
- A deregistered or released agent does not leave any room. It stays joined and in every member list, and goes quiet.
- A job that is dispatched while no client is connected posts a notice into the room rather than failing silently.

## References

- Server defaults and required keys: `element-agent/internal/server/config.go`
- Prompt composition: `element-agent/internal/daemon/prompt.go`
- Dispatch and history paging: `element-agent/internal/server/dispatch.go`
- The rest of the stack, and the Caddyfile this route is added to: [deployment.md](deployment.md)
- Accounts, rooms, and the admin API these steps use: [operations.md](operations.md)
- The session-based alternative to one-shot dispatch, not built: [future-claude-code-channels.md](future-claude-code-channels.md)
