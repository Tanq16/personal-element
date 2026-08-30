# Deployment

Everything needed to rebuild this stack from a bare VPS. Every file is here in full, with the lines that are load bearing called out, because most of them fail in ways that point somewhere other than the cause.

The order matters in two places and is otherwise free: Synapse's signing key has to be generated before the first `up`, and Postgres has to be initialised with the right collation before it holds any data.

## The shape of it

Six containers, one reverse proxy, three hostnames.

```
                              internet
                                 |
                          443/tcp (Caddy)
                                 |
        +------------------------+------------------------+
        |                        |                        |
element.example.com   element-chat.…           element-call.…
        |                        |                        |
   +----+-----+             element-web              element-call
   |    |     |             127.0.0.1:8081           127.0.0.1:8082
   |    |     |
   |    |     +-- /_matrix/*  ---------> synapse   127.0.0.1:8008
   |    |                                   |
   |    |                                postgres  (compose network only)
   |    |
   |    +-- /livekit/jwt/* -------------> lk-jwt   127.0.0.1:8080
   |
   +-- /livekit/sfu/*  ----------------> livekit  127.0.0.1:7880

                    LiveKit media, bypassing Caddy entirely:
                    7881/tcp and 50000-60000/udp, straight to the host
```

Only Caddy binds a public port. Everything else is on loopback, except LiveKit, where the firewall is the only thing holding the line.

## Prerequisites

- A VPS with a public IPv4 address, at least 2 cores and 4 GB of RAM. Voice is the expensive part, and it is CPU rather than memory.
- Docker and the compose plugin.
- A domain whose DNS you control, resolving publicly so Let's Encrypt can validate it.
- Caddy, or another reverse proxy. The Caddyfile below is short enough to translate.

This deployment runs on `element-arm`, a `VM.Standard.A1.Flex` with 2 OCPU, 12 GB, and a 200 GB boot volume, on Ubuntu 24.04 aarch64 in `us-ashburn-1`. The stack idles at about 240 MB.

## DNS

Five A records point at `203.0.113.10`. Three belong to this stack:

| Host | Serves |
|---|---|
| `element.example.com` | Synapse, the LiveKit proxy paths, both well-known documents |
| `element-chat.example.com` | Element Web |
| `element-call.example.com` | Element Call standalone, for the phones |

`element-term.example.com` and `element-code.example.com` serve the web terminal and code-server, which share the same Caddy and are otherwise unrelated to this stack.

The first host is special. It becomes `server_name`, which is baked into every user ID, every room ID, and every event on the server, and it cannot be changed later without throwing the server away. User IDs read `@name:element.example.com` as a result. Getting `@name:example.com` instead would have needed an apex A record serving both well-known documents, and that decision has to be made before the first `generate`.

## Firewall

Four ports inbound and no more.

| Port | Protocol | For |
|---|---|---|
| 22 | TCP | SSH |
| 443 | TCP | Caddy |
| 7881 | TCP | LiveKit ICE/TCP fallback |
| 50000-60000 | UDP | LiveKit WebRTC media |

Port 80 stays closed. Caddy issues certificates over TLS-ALPN-01 on 443, so the HTTP challenge is never used and there is nothing to redirect.

**Port 7880 must never be opened.** LiveKit runs on host networking, so its HTTP API binds `0.0.0.0:7880` whether or not you want it to, and the firewall is the only thing between that API and the internet. The proxy reaches it over loopback.

**7881/tcp is not optional.** Clients on a UDP-hostile network fall back to ICE over TCP. Close it and those people silently cannot join calls.

On this host the rules live in the OCI security list `Default Security List for element-vcn`, with ingress from `0.0.0.0/0`. Oracle's Ubuntu image also ships `/etc/iptables/rules.v4` carrying a catch-all `-A INPUT -j REJECT` that duplicates the security list; it was removed from both the live table and the file, leaving `INPUT` policy `ACCEPT`. The `InstanceServices` chain on `OUTPUT` was left alone, because it restricts `169.254.0.0/16` so non-root users cannot reach the iSCSI target backing the boot volume, and Oracle's guidance is to keep it.

Do not use `ufw` on that image. Oracle states it can leave an instance unable to boot.

## Directory layout

```
~/element/
  .env                                   generated secrets, mode 600
  compose.yaml
  livekit.yaml                           API key pair, mode 600
  element-web/config.json
  element-web/element-call-config.json   mounted over the shipped widget config
  element-call/config.json               standalone SPA config
  data/synapse/homeserver.yaml           mode 600
  data/synapse/*.signing.key             server identity, back this up
  data/postgres/
```

## Secrets

Generate them before anything else.

```sh
mkdir -p ~/element/{element-web,element-call,data/synapse,data/postgres}
cd ~/element

docker run --rm livekit/livekit-server:v1.13.6 generate-keys
```

That prints an API key and secret pair. Put them and a Postgres password into `.env`:

```sh
cat > .env <<EOF
POSTGRES_PASSWORD=$(openssl rand -hex 24)
LIVEKIT_KEY=<the key generate-keys printed>
LIVEKIT_SECRET=<the secret generate-keys printed>
EOF
chmod 600 .env
```

`.env`, `livekit.yaml`, and `data/synapse/homeserver.yaml` all hold secrets at mode 600, and none of them belongs in a git repository.

## compose.yaml

```yaml
name: element

x-logging: &logging
  driver: journald
  options:
    tag: "{{.Name}}"

services:
  postgres:
    image: postgres:17.9-alpine
    restart: unless-stopped
    logging: *logging
    environment:
      POSTGRES_USER: synapse
      POSTGRES_DB: synapse
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:?set in .env}
      POSTGRES_INITDB_ARGS: "--encoding=UTF-8 --lc-collate=C --lc-ctype=C"
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U synapse -d synapse"]
      interval: 10s
      timeout: 5s
      retries: 10

  synapse:
    image: ghcr.io/element-hq/synapse:v1.159.0
    restart: unless-stopped
    logging: *logging
    depends_on:
      postgres:
        condition: service_healthy
    environment:
      SYNAPSE_CONFIG_PATH: /data/homeserver.yaml
      UID: "1001"
      GID: "1001"
      TZ: America/Toronto
    volumes:
      - ./data/synapse:/data
    ports:
      - "127.0.0.1:8008:8008"

  livekit:
    image: livekit/livekit-server:v1.13.6
    restart: unless-stopped
    logging: *logging
    network_mode: host
    command: --config /etc/livekit.yaml
    volumes:
      - ./livekit.yaml:/etc/livekit.yaml:ro

  lk-jwt:
    image: ghcr.io/element-hq/lk-jwt-service:0.6.0
    restart: unless-stopped
    logging: *logging
    depends_on:
      - livekit
    environment:
      LIVEKIT_URL: wss://element.example.com/livekit/sfu
      LIVEKIT_KEY: ${LIVEKIT_KEY:?set in .env}
      LIVEKIT_SECRET: ${LIVEKIT_SECRET:?set in .env}
      LIVEKIT_FULL_ACCESS_HOMESERVERS: element.example.com
      LIVEKIT_SANITY_CHECK_INTERVAL_SECONDS: "60"
    ports:
      - "127.0.0.1:8080:8080"

  element-web:
    image: vectorim/element-web:v1.12.26
    restart: unless-stopped
    logging: *logging
    environment:
      ELEMENT_WEB_PORT: "8080"
    volumes:
      - ./element-web/config.json:/app/config.json:ro
      - ./element-web/element-call-config.json:/app/widgets/element-call/config.json:ro
    ports:
      - "127.0.0.1:8081:8080"

  element-call:
    image: ghcr.io/element-hq/element-call:v0.24.0
    restart: unless-stopped
    logging: *logging
    volumes:
      - ./element-call/config.json:/app/config.json:ro
    ports:
      - "127.0.0.1:8082:8080"
```

**`UID` and `GID` must match the host user.** They are 1001 here because that is what `ubuntu` is on this image. Run `id -u` and `id -g` and use those numbers; guess wrong and Synapse writes files you cannot read.

**`POSTGRES_INITDB_ARGS` is not optional.** Synapse refuses to start against a database with any collation other than `C`, and the setting only takes effect the first time the data directory is initialised. Getting it wrong means dumping, dropping, recreating, and restoring.

Every service carries `restart: unless-stopped`, which is what brings the stack back after a reboot without a systemd unit. Every service logs to journald rather than the default `json-file` driver, which grows without bound.

## Synapse

The signing key is created during `generate` and cannot be invented afterwards, so this runs before the first `up`. It is the server's identity, and losing it means every client re-verifies everything.

```sh
cd ~/element
docker run --rm -v ~/element/data/synapse:/data \
  -e SYNAPSE_SERVER_NAME=element.example.com \
  -e SYNAPSE_REPORT_STATS=no -e UID=1001 -e GID=1001 \
  ghcr.io/element-hq/synapse:v1.159.0 generate
```

That writes `homeserver.yaml`, a log config, and `element.example.com.signing.key`. Keep the generated `registration_shared_secret`, `macaroon_secret_key`, and `form_secret`; the file below preserves them.

### homeserver.yaml

```yaml
server_name: "element.example.com"
public_baseurl: "https://element.example.com/"
pid_file: /data/homeserver.pid
signing_key_path: "/data/element.example.com.signing.key"
log_config: "/data/element.example.com.log.config"
media_store_path: /data/media_store
report_stats: false

serve_server_wellknown: true
suppress_key_server_warning: true
trusted_key_servers:
  - server_name: "matrix.org"

listeners:
  - port: 8008
    tls: false
    type: http
    x_forwarded: true
    bind_addresses: ['0.0.0.0']
    resources:
      - names: [client, openid]
        compress: false

database:
  name: psycopg2
  args:
    user: synapse
    password: "<POSTGRES_PASSWORD from .env>"
    dbname: synapse
    host: postgres
    port: 5432
    cp_min: 5
    cp_max: 10

enable_registration: false
allow_guest_access: false
registration_shared_secret: "<generated>"
macaroon_secret_key: "<generated>"
form_secret: "<generated>"

experimental_features:
  msc3266_enabled: true
  msc4143_enabled: true
  msc4222_enabled: true

max_event_delay_duration: 24h

rc_message:
  per_second: 0.5
  burst_count: 30

rc_delayed_event_mgmt:
  per_second: 1
  burst_count: 20

matrix_rtc:
  transports:
    - type: livekit
      livekit_service_url: https://element.example.com/livekit/jwt

federation_domain_whitelist: []
```

Every non-default line in there is load bearing.

**`resources: [client, openid]`, and why `openid` has to be spelled out.** The obvious way to disable federation is to drop `federation` from the resource list, and that is correct as far as it goes. The trap is that Synapse's `federation` resource silently implies `media`, `keys`, and `openid`. Remove it without naming `openid` explicitly and you also remove `/_matrix/client/v3/user/{userId}/openid/request_token` and the `/_matrix/federation/v1/openid/userinfo` endpoint that validates the resulting token, which is exactly what lk-jwt-service uses to decide who may enter a call. Voice then breaks with an error pointing nowhere near federation.

**`federation_domain_whitelist: []`** is the belt to the listener's braces. An empty allow-list permits no remote server even if a federation listener somehow became reachable.

**`serve_server_wellknown: true`** publishes `/.well-known/matrix/server`. With federation off it does nothing, and it costs one line, so it stays in case federation is ever turned on.

**`msc4143_enabled: true`** registers `GET /_matrix/client/unstable/org.matrix.msc4143/rtc/transports`, which is how clients discover the SFU, and `matrix_rtc.transports` is what that endpoint returns. Without the flag the endpoint 404s and clients fall back to a config file.

**`msc3266_enabled: true`** provides the room summary endpoint clients use to preview a room before joining. **`msc4222_enabled: true`** adds `state_after` to sync, which Element X wants.

**`max_event_delay_duration: 24h` and `rc_delayed_event_mgmt`.** MatrixRTC uses delayed events to clear call membership when a client disappears without saying goodbye: the client schedules its own "I have left" event and keeps postponing it while it is still connected. Without a delay allowance a crashed client leaves a ghost in the call forever. The rate limit exists because every connected participant restarts its delayed event every few seconds.

**`rc_message: per_second: 0.5, burst_count: 30`** is the default and it is why the element-agent appservice registration sets `rate_limited: false`. An agent posting a chunked answer would hit it.

**Nothing about encryption.** `encryption_enabled_by_default_for_room_type` is deliberately absent, because its default is already `"off"`. Writing `off` in YAML hands Synapse a boolean where it expects a string and it refuses to start. Unencrypted channels are what make bots simple and backups readable, and Element still encrypts direct messages client-side regardless.

## LiveKit

`~/element/livekit.yaml`, mode 600:

```yaml
port: 7880
rtc:
  tcp_port: 7881
  port_range_start: 50000
  port_range_end: 60000
  use_external_ip: true
keys:
  <LIVEKIT_KEY>: <LIVEKIT_SECRET>
room:
  auto_create: false
webhook:
  api_key: <LIVEKIT_KEY>
  urls:
    - http://127.0.0.1:8080/sfu_webhook
```

**Host networking, not port publishing.** Publishing `50000-60000/udp` through Docker means ten thousand port mappings and a `docker-proxy` process for each. LiveKit's own documentation says to use host networking in Docker. The cost is that `port: 7880` binds `0.0.0.0` and only the firewall closes it.

**`use_external_ip: true` is required on a cloud VPS.** The interface here carries only `10.0.1.31`, with the public address applied by 1:1 NAT outside the guest. LiveKit discovers the real address over STUN at startup. Without this it advertises the private address in ICE candidates and nobody outside the VPC can connect.

**The `webhook:` block is not optional.** LiveKit posts room and participant lifecycle events to lk-jwt-service, which is how lk-jwt learns that somebody has actually left a call. `api_key` has to name one of the keys under `keys:`, because the SFU signs every payload with the matching secret and lk-jwt verifies the signature. LiveKit is on host networking and lk-jwt publishes on loopback, so the two talk over `127.0.0.1` without a round trip through the proxy. Omit this and calls appear to work perfectly right up until the first client dies without hanging up, which is diagnosed in [operations.md](operations.md#a-call-that-never-ends).

**`room: auto_create: false` is what makes the access rules mean anything.** `LIVEKIT_FULL_ACCESS_HOMESERVERS` governs only which homeservers lk-jwt will create SFU rooms for. LiveKit itself creates a room for anyone presenting a valid join token unless auto-create is off, so without this the restriction decides nothing. lk-jwt calls `CreateRoom` explicitly when it issues a token, so turning auto-create off costs nothing on the normal path.

## lk-jwt-service

This is the MatrixRTC authorisation service, and it is the piece connecting Matrix identity to LiveKit access.

```
1. client  → Synapse   POST /_matrix/client/v3/user/{userId}/openid/request_token
2. client  → lk-jwt    that token, plus the room it wants
3. lk-jwt  → Synapse   GET /_matrix/federation/v1/openid/userinfo   (validate)
4. lk-jwt  → client    a LiveKit JWT scoped to that room, plus the SFU WebSocket URL
```

It is configured entirely through environment variables in `compose.yaml`. `LIVEKIT_URL` is the address handed to clients, so it has to be the public proxied path rather than the loopback one. `LIVEKIT_FULL_ACCESS_HOMESERVERS` names which homeservers get full participant rights rather than guest treatment; with federation off that is only ever this one. `LIVEKIT_SANITY_CHECK_INTERVAL_SECONDS: "60"` makes every delegated-leave job ask the SFU once a minute whether its participant is still there, and it defaults to `0`, which disables it.

## Element Web

`~/element/element-web/config.json`:

```json
{
  "default_server_config": {
    "m.homeserver": {
      "base_url": "https://element.example.com",
      "server_name": "element.example.com"
    }
  },
  "element_call": { "use_exclusively": true },
  "setting_defaults": { "enableLegacyCallsVoip": false },
  "features": {
    "feature_video_rooms": true,
    "feature_element_call_video_rooms": true
  },
  "disable_custom_urls": true,
  "disable_guests": true,
  "show_labs_settings": true
}
```

`element_call.use_exclusively` and `setting_defaults.enableLegacyCallsVoip` together force every call through Element Call. Element otherwise offers a legacy peer-to-peer call in any room with two or fewer members, and legacy WebRTC needs a TURN server, which this deployment deliberately does not have. `feature_video_rooms` and `feature_element_call_video_rooms` are what turn a room into a persistent voice channel you drop into rather than a call you start. `disable_custom_urls` removes the homeserver picker from the login screen, so this deployment can only ever be used against this homeserver.

`~/element/element-web/element-call-config.json`, mounted over `/app/widgets/element-call/config.json`:

```json
{
  "matrix_rtc_session": {
    "wait_for_key_rotation_ms": 5000,
    "delayed_leave_event_restart_ms": 4000,
    "delayed_leave_event_delay_ms": 18000
  },
  "livekit": { "livekit_service_url": "https://element.example.com/livekit/jwt" }
}
```

The mount replaces the shipped file wholesale rather than merging with it, so the `matrix_rtc_session` block has to stay even though those are the shipped defaults. The shipped file carries no `livekit` key at all, and adding one is the entire reason Element Web is self-hosted here.

## Element Call standalone

`~/element/element-call/config.json`, for the `element-call.example.com` container:

```json
{
  "default_server_config": {
    "m.homeserver": {
      "base_url": "https://element.example.com",
      "server_name": "element.example.com"
    }
  },
  "matrix_rtc_session": {
    "wait_for_key_rotation_ms": 5000,
    "delayed_leave_event_restart_ms": 4000,
    "delayed_leave_event_delay_ms": 18000
  },
  "livekit": { "livekit_service_url": "https://element.example.com/livekit/jwt" }
}
```

This host exists entirely for the phones, which bundle their own copy of Element Call carrying no `livekit` key and need somewhere to be pointed at that does.

## Caddy

Caddy is not managed by compose and is not a systemd service here. It runs from a tmux session alongside the web terminal and code-server, which means it is the one component with no restart policy and needs starting by hand after a reboot.

```sh
sudo setcap cap_net_bind_service=+ep /home/ubuntu/shell/extensions/caddy
caddy run --config ~/Caddyfile
```

The capability is lost every time the binary is replaced, so it needs reapplying after an update.

```caddyfile
{
	email <ACME contact address>
	admin off
	cert_issuer acme {
		disable_http_challenge
	}
	auto_https disable_redirects
	servers {
		timeouts {
			read_header 10s
		}
	}
}

element.example.com {
	handle_path /livekit/jwt/* {
		reverse_proxy 127.0.0.1:8080
	}
	handle_path /livekit/sfu/* {
		reverse_proxy 127.0.0.1:7880
	}
	handle /.well-known/element/element.json {
		header Content-Type application/json
		header Access-Control-Allow-Origin *
		respond `{"call":{"widget_url":"https://element-call.example.com"}}`
	}
	@matrix path /_matrix/* /_synapse/client/* /.well-known/matrix/*
	handle @matrix {
		reverse_proxy 127.0.0.1:8008
	}
	handle {
		respond 404
	}
}

element-chat.example.com {
	header {
		X-Content-Type-Options nosniff
		X-Frame-Options SAMEORIGIN
		Content-Security-Policy "frame-ancestors 'self'"
		Referrer-Policy no-referrer
		-Server
	}
	reverse_proxy 127.0.0.1:8081
}

element-call.example.com {
	header {
		X-Content-Type-Options nosniff
		Referrer-Policy no-referrer
		-Server
	}
	reverse_proxy 127.0.0.1:8082
}
```

Five things in that file are doing real work.

**`/_synapse/admin/*` is deliberately absent from the `@matrix` matcher.** Synapse serves its entire admin API on the same listener as the client API, and that API can create users, deactivate accounts, and read any room. Matching only `/_matrix/*`, `/_synapse/client/*`, and `/.well-known/matrix/*`, then answering 404 to everything else, leaves the admin API reachable only at `127.0.0.1:8008` and only to someone already on the box. This is the single highest-value line in the deployment.

**The catch-all `handle { respond 404 }`** means an unmatched path never reaches Synapse. Combined with the absence of a host-less site block, a request to the bare IP presents no SNI and dies during the TLS handshake, so the server never advertises what it is running.

**`handle_path` strips the matched prefix**, which is what lk-jwt-service and LiveKit both expect. Caddy will not parse a `handle_path` block written on one line; the braces need their own lines.

**`X-Frame-Options SAMEORIGIN` on the Element Web host, not `DENY`.** Element Call runs as an iframe inside Element Web from the same origin, and `DENY` breaks calls.

**`admin off` disables Caddy's admin endpoint**, which is also what `caddy reload` talks to. Adding a route therefore means restarting Caddy rather than reloading it, and anything documented elsewhere as a `caddy reload` has to account for that.

## Bringing it up

```sh
cd ~/element
docker compose up -d
docker compose ps
```

Then start Caddy. On first run it obtains three certificates over TLS-ALPN-01.

## Verifying it, before trusting it

Each of these tests one specific claim. Run all of them.

```sh
# Element Web is served and has its config
curl -s -o /dev/null -w '%{http_code}\n' https://element-chat.example.com/config.json

# The mounted widget config really did replace the shipped one
curl -s https://element-chat.example.com/widgets/element-call/config.json | jq .livekit

# Standalone Element Call has the same SFU
curl -s https://element-call.example.com/config.json | jq .livekit

# Client discovery points at the homeserver
curl -s https://element.example.com/.well-known/matrix/client | jq .

# Federation is actually off; expect 404
curl -s -o /dev/null -w '%{http_code}\n' https://element.example.com/_matrix/federation/v1/version

# The admin API is not exposed publicly; expect 404 here and 200 on loopback
curl -s -o /dev/null -w '%{http_code}\n' https://element.example.com/_synapse/admin/v1/server_version
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8008/_synapse/admin/v1/server_version

# Registration is closed
curl -s -XPOST https://element.example.com/_matrix/client/v3/register -d '{}'

# The SFU is advertised to authenticated clients
curl -s -H "Authorization: Bearer $TOKEN" \
  https://element.example.com/_matrix/client/unstable/org.matrix.msc4143/rtc/transports | jq .

# The webhook route exists; expect 200 plus a signature complaint in the lk-jwt log
curl -s -o /dev/null -w '%{http_code}\n' -X POST -d '{}' http://127.0.0.1:8080/sfu_webhook
```

Get `$TOKEN` by logging in over loopback:

```sh
curl -s -XPOST http://127.0.0.1:8008/_matrix/client/v3/login \
  -d '{"type":"m.login.password","identifier":{"type":"m.id.user","user":"admin"},"password":"..."}' \
  | jq -r .access_token
```

The webhook check earns a word of explanation, because a bare `200` looks like success either way. What proves the route is live is the matching line in `docker compose logs lk-jwt`: `SFU webhook error err=authorization header could not be found`. That is the signature check rejecting an unsigned probe, which is the correct response. A `404` means the path is wrong, and silence means the request never arrived. To prove the whole round trip rather than just the route, create and delete a throwaway SFU room through the LiveKit API and look for `sent webhook` with `"statusCode": 200` in the LiveKit log.

The end-to-end RTC check exercises every piece of the call path except the media itself, and should return a `url` and a `jwt`:

```sh
OIDC=$(curl -s -XPOST -H "Authorization: Bearer $TOKEN" \
  "http://127.0.0.1:8008/_matrix/client/v3/user/%40admin%3Aelement.example.com/openid/request_token" -d '{}')

jq -nc --argjson o "$OIDC" '{room:"test", openid_token:$o, device_id:"TEST"}' \
  | curl -s -XPOST https://element.example.com/livekit/jwt/sfu/get \
      -H 'Content-Type: application/json' -d @-
```

Finally, scan from outside the box. Ports 22, 443, and 7881 should be open, and 80, 7880, 8008, 8080, 8081, and 8082 filtered.

## The security posture, in one place

- **Nothing but Caddy binds a public port**, except LiveKit's media ports and its API on 7880, which the firewall closes.
- **The admin API is unreachable from outside the host.** The proxy matcher does not include it and the catch-all answers 404.
- **Federation is off at the listener and by allow-list.** No remote server can read, join, or query anything.
- **Registration and guest access are off.** Accounts exist only because an admin created them.
- **Port 80 is closed.** Certificates are issued over TLS-ALPN-01 on 443.
- **A bare-IP request fails at the TLS handshake**, because no site block matches an empty SNI.
- **Secrets live in `.env`, `livekit.yaml`, and `homeserver.yaml`, all mode 600.** None of them belongs in a git repository.
- **One account holds server admin.** Everyone else, including every agent, is an ordinary user.

## Journald retention

The compose logging driver is journald, so retention is a host setting rather than a per-container one.

```sh
sudo mkdir -p /etc/systemd/journald.conf.d
sudo tee /etc/systemd/journald.conf.d/99-retention.conf >/dev/null <<EOF
[Journal]
Storage=persistent
MaxRetentionSec=90day
SystemMaxUse=8G
EOF
sudo systemctl restart systemd-journald
```

## References

- Day-to-day administration, backup, and the ghost-call runbook: [operations.md](operations.md)
- What each person does on their own device: [clients.md](clients.md)
- The agent application service, which is not deployed: [element-agent.md](element-agent.md)
