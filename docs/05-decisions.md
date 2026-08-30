# Decisions

Why this stack rather than another, and why each load-bearing setting is the way it is. Every entry records a choice that is expensive to reverse or that looks wrong until the reason is stated.

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

Publishing `50000-60000/udp` through Docker means ten thousand port mappings and a `docker-proxy` for each. LiveKit's own docs say host networking should be used in Docker. The side effect is that LiveKit's API binds `0.0.0.0:7880`, and the firewall is the only thing closing it, which is why 7880 must never be opened.

`use_external_ip: true` is required. The VM's interface only carries `10.0.1.31`; the public address is 1:1 NAT, and LiveKit discovers it over STUN at startup.

### SFU webhooks

LiveKit posts room and participant events to lk-jwt at `http://127.0.0.1:8080/sfu_webhook`, signed with the API key from `livekit.yaml`. LiveKit runs on host networking, so that loopback address reaches the published lk-jwt port directly rather than going back out through Caddy.

Without the webhook block, lk-jwt never learns that a participant disconnected, and a browser tab closed uncleanly leaves behind a call membership that nothing clears. Every client in the room then shows a permanent ongoing call that the phones refuse to join. The full mechanism and the two queries that diagnose it are in [docs/02-operations.md](02-operations.md#a-call-that-never-ends).

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

## References

- Standing the stack up, and every configuration file: [01-deployment.md](01-deployment.md)
- Day-to-day administration and the runbooks: [02-operations.md](02-operations.md)
- What each person does on their own device: [03-clients.md](03-clients.md)
- The agent application service: [04-element-agent.md](04-element-agent.md)
