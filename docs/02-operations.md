# Operations

Running the server after it is up: accounts, rooms, logs, backup, upgrades, and the one failure that looks like a client bug and is not.

Every command here runs on the host. The admin API is bound to `127.0.0.1:8008` and is not reachable through the proxy, so these work from the web terminal and from nowhere else. Get an admin token by logging in over loopback:

```sh
TOKEN=$(curl -s -XPOST http://127.0.0.1:8008/_matrix/client/v3/login \
  -d '{"type":"m.login.password","identifier":{"type":"m.id.user","user":"admin"},"password":"..."}' \
  | jq -r .access_token)
```

`nonrefreshable_access_token_lifetime` and `session_lifetime` both default to `infinity`, so that token does not expire. Do not log in per action: every login mints a new device and the session list fills with junk that never goes away.

The `@` and `:` in a user ID percent-encode as `%40` and `%3A`, and the `!` starting a room ID as `%21`.

## Accounts

Registration is off, so accounts are made on the host.

```sh
cd ~/element
docker compose exec synapse register_new_matrix_user \
  -c /data/homeserver.yaml --no-admin http://localhost:8008
```

It prompts for the localpart and a password.

`--no-admin` is the default to reach for. Server admin is a flag on the user row granting the entire `/_synapse/admin` API, and it has nothing to do with power levels inside any room. Only `@admin:element.example.com` holds it.

Promote or demote later:

```sh
curl -s -XPUT "http://127.0.0.1:8008/_synapse/admin/v2/users/%40alice%3Aelement.example.com" \
  -H "Authorization: Bearer $TOKEN" -d '{"admin": false}' | jq
```

## Spaces, channels, and access

One space holds every channel. In Element: the `+` in the space panel, then Create a space, then add rooms to it.

Access is per room, under Room Settings, Security & Privacy, Access:

| Option | Meaning here |
|---|---|
| Space members | Anyone in the space joins freely. The right default for channels. |
| Invite only | Only explicitly invited people. |
| Public | Identical to Space members, because federation is off. |

Read-only is a separate control, under Roles & Permissions, by raising the "Send messages" threshold above Default.

The per-room checkbox "Block anyone not part of element.example.com from ever joining this room" writes `m.federate: false` into the room's creation event and cannot be undone afterwards. With federation already off server-wide it is redundant.

Non-admins can create their own spaces, and Synapse has no setting preventing it. `rc_room_creation` only rate-limits. Blocking it would need the third-party `synapse-user-restrictions` module, from which server admins are exempt anyway.

## Rooms

Element has no delete. Leaving a room only removes you. Actually removing one takes the admin API.

```sh
curl -s "http://127.0.0.1:8008/_synapse/admin/v1/rooms" -H "Authorization: Bearer $TOKEN" | jq

curl -s -XDELETE "http://127.0.0.1:8008/_synapse/admin/v2/rooms/%21ROOMID%3Aelement.example.com" \
  -H "Authorization: Bearer $TOKEN" -d '{"block":false,"purge":true}' | jq
```

The v2 endpoint is asynchronous and returns a `delete_id`; poll `GET /_synapse/admin/v2/rooms/<room_id>/delete_status`. Add `"force_purge": true` if local users are still joined, and `"block": true` to stop anyone rejoining that room ID.

## Bots outside the appservice

Agents mentioned by name run through the element-agent application service and hold no credential of their own. A plain bot is different: it is an ordinary account holding an ordinary token, and it is still the right shape for something that only needs to post.

```sh
curl -s -XPOST http://127.0.0.1:8008/_matrix/client/v3/login \
  -d '{"type":"m.login.password","identifier":{"type":"m.id.user","user":"agent-goose"},"password":"..."}'
```

Keep that token rather than logging in per action, for the same device-list reason as above. Pass `"refresh_token": true` at login if you want short-lived credentials instead; the access token then lives 5 minutes and is exchanged at `POST /_matrix/client/v3/refresh`.

Scope such a bot by room membership, inviting it only to the channels it should see. Do not add one to the space while channels use "Space members" access, because it inherits every channel in the space. To make it read-only, set its power level below the room's "Send messages" threshold.

Revoke with its own token at `POST /_matrix/client/v3/logout`, or from the admin side:

```sh
curl -s -XDELETE \
  "http://127.0.0.1:8008/_synapse/admin/v2/users/%40agent-goose%3Aelement.example.com/devices/<device_id>" \
  -H "Authorization: Bearer $TOKEN"
```

Deactivating the account kills every token it holds at once. It does not free the localpart: deactivation deletes tokens, devices, and 3PIDs and removes the user from all rooms, while the user row persists and is reactivatable.

## A call that never ends

This is the failure that costs an evening if you have not seen it before, because it presents as a phone problem and is a server one.

Presence in a call is a state event in the room, `org.matrix.msc3401.call.member`, keyed by user and device. Non-empty content means you are in the call and `{}` means you left. A client that hangs up cleanly writes `{}` itself. A client that dies without warning, a closed laptop, a killed browser tab, a phone that lost signal, writes nothing, and that membership would otherwise sit there forever.

MSC4140 delayed events are the answer. On joining, the client registers a teardown with the homeserver: if I stop checking in for eighteen seconds, write `{}` on my behalf. It then restarts that timer every few seconds for as long as it is in the call.

That leaves a gap, because a browser tab that is backgrounded, throttled, or briefly offline stops restarting the timer and would be ejected from a call it is still in. So lk-jwt-service takes the job over: while it believes you are connected to the SFU it restarts your delayed event for you, and it stops the moment the SFU reports you gone.

The whole design therefore hinges on lk-jwt learning that you disconnected, and it learns that from the LiveKit webhook. Leave the `webhook:` block out of `livekit.yaml` and it never finds out. It goes on restarting the teardown every fourteen seconds, indefinitely, for a participant who left hours ago, and the membership never clears.

From the outside that looks like a call permanently in progress. Every client in the room shows a join prompt for a call nobody is on. Browser clients join it anyway, because Element Web is content to join a call alongside a member it cannot see. The mobile clients refuse and show a generic error, which is what sends you off debugging the phones.

Two queries separate the two views. What the homeserver believes:

```sh
docker compose exec -T postgres psql -U synapse -d synapse -tA -F'|' \
  -c "SELECT delay_id, user_localpart, device_id, send_ts FROM delayed_events;"
```

A row whose `send_ts` advances every time you run it is a timer something is actively holding open. Then what the SFU believes, over the same window:

```sh
docker compose logs livekit --since 1h --no-log-prefix \
  | grep -E 'starting RTC session|participant closing|closing idle room'
```

If the device from the first output never appears in the second, and the room closed with `"reason": "departure timeout"`, then nobody is on that call and lk-jwt is holding a ghost. Confirm who is holding it from the user agent on the restart requests in the homeserver log: `Go-http-client/2.0` is lk-jwt, and a browser string is a real client that is genuinely still there.

Recovery is a restart:

```sh
docker compose restart lk-jwt
```

lk-jwt keeps its jobs in memory unless given `LIVEKIT_REDIS_URL`, so the orphaned job dies with the process. The homeserver fires the pending delayed event within seconds and the membership clears down the normal path, with no manual state surgery and nothing to redact.

Prevention is the `webhook:` block plus `LIVEKIT_SANITY_CHECK_INTERVAL_SECONDS: "60"` on lk-jwt, which is a pull-based fallback for a webhook that is merely unreliable rather than absent.

## Logs

Containers log to journald with `tag: {{.Name}}`.

```sh
docker compose logs -f synapse
journalctl CONTAINER_TAG=element-synapse-1 -f
```

The journald path survives container recreation and the compose path does not, which matters the moment you upgrade an image and want to compare before and after.

When a call fails, the two logs that actually say why are `docker compose logs livekit` and `docker compose logs lk-jwt`, read across the window of the attempt.

## Backup

Four things need copying off the host. Everything else is reproducible from this repository.

| What | Where | Size here | Changes |
|---|---|---|---|
| Server identity | `data/synapse/*.signing.key` | 59 bytes | never |
| Generated secrets | `.env`, `livekit.yaml`, `data/synapse/homeserver.yaml` | 2 KB | only on rotation |
| Database | `pg_dump` of `synapse` | 3 MB, 1 MB gzipped | every message |
| Uploads | `data/synapse/media_store` | 19 MB | every upload |

The signing key is the one with no substitute. Restore the other three without it and Synapse comes up as a cryptographically different server under the same name, so existing events fail signature checks and every device has to verify again.

Because channels are unencrypted, the dump is a readable archive of everything ever said, which is the point of running them that way. That holds only for rooms that are actually unencrypted. Anything sent in a room where someone enabled encryption is stored as ciphertext and stays unreadable through a restore, since the keys live on members' devices rather than on the server.

There is nowhere on this host to keep any of it. The whole disk is the boot volume, so every backup has to leave the machine.

### Taking one

Run these from a workstation with SSH access. Nothing needs installing on the host, and nothing is written to it.

```sh
ssh ubuntu@<host> 'cd ~/element && docker compose exec -T postgres pg_dump -U synapse synapse' \
  | gzip > synapse-$(date +%F).sql.gz

ssh ubuntu@<host> 'tar czf - -C ~/element/data/synapse media_store' \
  > media-$(date +%F).tar.gz

ssh ubuntu@<host> 'cd ~/element && tar cf - .env livekit.yaml \
  data/synapse/homeserver.yaml data/synapse/*.signing.key' \
  > config-$(date +%F).tar
chmod 600 config-$(date +%F).tar
```

The first two are worth taking on a schedule. The third holds every secret on the host in plain text, changes only when something is rotated, and belongs somewhere encrypted.

The database dump is consistent without stopping anything, because `pg_dump` runs in a single transaction. The media tarball is not, so a file uploaded while it runs may be missing from it; the next one picks it up.

### Restoring

Stand the host up through [01-deployment.md](01-deployment.md) as far as generating the directory layout, then stop before the first `docker compose up`.

```sh
tar xf config-<date>.tar -C ~/element
tar xzf media-<date>.tar.gz -C ~/element/data/synapse
```

The signing key has to be in place before Synapse first starts, or it generates a new one and the identity is lost for good.

```sh
docker compose up -d postgres
gunzip -c synapse-<date>.sql.gz | docker compose exec -T postgres psql -U synapse -d synapse
docker compose up -d
```

**The new Postgres cluster must be initialised with `--lc-collate=C --lc-ctype=C`**, which the `POSTGRES_INITDB_ARGS` line in `compose.yaml` does on first start. A dump restored into a default-collation cluster loads without complaint and then misbehaves on ordering, which is slow to diagnose and needs a full reload to fix.

Then run every check under "Verifying it, before trusting it" in [01-deployment.md](01-deployment.md).

## Upgrades

Bump the tag in `compose.yaml`, then:

```sh
docker compose pull
docker compose up -d
```

Synapse runs its own database migrations at startup. Take a `pg_dump` first anyway. Never skip a major Synapse version, and read its upgrade notes, which occasionally require action before the new version will start.

Naming a single service confines the change to it, which is how the element-agent container is deployed without touching anything else:

```sh
docker compose up -d --build element-agent
```

## Verifying cross-signing state

If you are unsure whether recovery was ever set up on an account:

```sh
docker compose exec -T postgres psql -U synapse -d synapse -c \
  "SELECT user_id, account_data_type, left(content,80) FROM account_data
   WHERE account_data_type LIKE 'm.secret_storage%' OR account_data_type LIKE 'm.cross_signing%';"
```

An `m.secret_storage.default_key` row whose content is `{}` means secret storage was torn down rather than set up. Real secret storage has a matching `m.secret_storage.key.<id>` row and the encrypted `m.cross_signing.*` secrets alongside it.

## References

- Standing the stack up from nothing, and every config file: [01-deployment.md](01-deployment.md)
- What each person does on their own device: [03-clients.md](03-clients.md)
- The agent application service: [04-element-agent.md](04-element-agent.md)
