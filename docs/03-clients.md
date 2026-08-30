# Clients

What each person does on their own device, and why three of the four clients need a setting nobody would guess.

Text works everywhere with no configuration. Calls need the client to know where the SFU is, and only the browser client learns that on its own.

| Client | Where to point it | Calls work out of the box |
|---|---|---|
| Element Web | `https://element-chat.example.com` | yes |
| Element X, Android and iOS | the homeserver, plus a per-device override | no, needs the override |
| Element Desktop | the homeserver, plus a per-machine override | no, needs the override |
| `app.element.io` | not usable | no, and it cannot be fixed |

## Why app.element.io does not work

The tempting shortcut is to skip self-hosting Element Web, on the reasoning that `app.element.io` is static JavaScript that talks to whatever homeserver you point it at. That is true for text and false for calls, for two independent reasons that compound.

**Legacy calls need TURN, which this deployment does not have.** Element offers a legacy peer-to-peer call in any room with two or fewer members, and legacy WebRTC needs a TURN server to cross symmetric NAT. There is none here on purpose, because the SFU makes it unnecessary for the group calls that matter. Forcing everything through Element Call instead takes `element_call.use_exclusively` and `setting_defaults.enableLegacyCallsVoip`, and you cannot change someone else's deployment's config file. Two people in a room get the legacy path and it fails.

**Element Call has to be told where the SFU is.** It resolves that in two steps, in order: the homeserver's `/_matrix/client/unstable/org.matrix.msc4143/rtc/transports` endpoint, then its own `config.json`. Step one is the right answer and this homeserver answers it. Step two is the fallback, and it reads `/widgets/element-call/config.json`, which in the shipped Element Web image contains only `matrix_rtc_session` timings with no `livekit` key at all. Mounting a replacement over that file is the only lever, and it means running the container yourself.

## Element Web

Open `https://element-chat.example.com` and sign in. The login screen offers no homeserver picker, because `disable_custom_urls` is set, so this deployment can only be used against this homeserver.

Nothing else is needed. Element Call runs as a widget inside the page and discovers the SFU from the transports endpoint, falling back to the mounted config if it cannot.

There is a subtlety behind that discovery worth knowing when it misbehaves: a widget holds no access token, so it cannot make the authenticated transports request itself. It asks the hosting client over the widget API instead, which needs `matrix-widget-api` 1.18.0 or newer at both ends. When both halves are new enough this is seamless, and when they are not, the mounted `livekit` key is what keeps calls working.

## Element X on phones

Element X does not fetch Element Call over the network. Android renders a bundle shipped inside the APK, from `https://appassets.androidplatform.net/element-call/index.html`, and iOS uses an equivalent bundled asset. That bundle carries no `livekit_service_url`, and it ignores `/.well-known/element/element.json` entirely. Each device has to be pointed at this deployment by hand.

**Reveal the setting.** Open Settings and tap the version text at the very bottom of the screen seven times in quick succession. On Android the counter resets after two seconds of inactivity, so the taps have to be fast. A **Developer options** entry appears at the foot of that same screen.

**Set the URL.**

| Platform | Field | Value |
|---|---|---|
| Android | Developer options, Element Call, base URL | `https://element-call.example.com/room` |
| iOS | Developer options, Element Call remote URL override | `https://element-call.example.com/room` |

**The `/room` path is required, and this is the trap.** Element Call's router maps `/` to its standalone home page, the one that asks you to name a call and then tries to create a room. In a widget WebView there is no access token, so pressing Go fails with a 401 on `POST /_matrix/client/v3/createRoom`, which looks nothing like a configuration problem. Every path other than `/` renders the widget view instead.

The path does a second job. Element Call special-cases a path ending in `/room` and fetches `config.json` from the origin root rather than from the current directory, which is exactly where the `livekit` key lives.

Element X does not use the transports endpoint yet. The version tested here bundles Element Call 0.23.0, and the widget-side discovery it would need only entered `matrix-widget-api` in v1.18.0, with the matching host support landing in `matrix-rust-sdk` shortly after. Once a build ships with both halves, the embedded bundle will ask the app, the app will query the transports endpoint, and this override becomes unnecessary. Until then it is the whole answer.

Across twelve hours of logs on this deployment, no Element X session requested the transports endpoint, wrote an `org.matrix.msc3401.call.member` event, or asked lk-jwt for a token, while the browser sessions did all three.

## Element Desktop

Same problem, same shape of answer. Element Desktop bundles its own Element Call inside the asar, and the override lives at `Developer.elementCallUrl`, which is a device-level setting no config file can reach. Set it per machine to the same `https://element-call.example.com/room`, or just use the browser.

## Device verification and recovery

Both Element Web and Element X warn that after a date they announce in-app, unverified sessions will no longer be able to send messages. This is worth getting right on day one, because the failure mode is unpleasant.

The first session to sign in creates the account's cross-signing identity. Every later session verifies against one that already holds it, by QR code or emoji comparison.

**Set up recovery on that first session, and store the recovery key somewhere off the machine.** The private cross-signing keys exist in exactly two places: on verified devices, and in server-side secret storage encrypted by the recovery key. Sign out of the last verified session without having set up recovery and both copies are gone. Element X then offers nothing but "Can't confirm", because there is no device left to verify against and no key to enter.

If that happens, the only way out is resetting the identity, which is cheap on a deployment like this one. Channels are unencrypted, so no channel history is lost, and only encrypted direct messages lose theirs. Other members see the identity change and verify again.

Checking from the server whether recovery was ever set up is in [02-operations.md](02-operations.md#verifying-cross-signing-state).

## The well-known documents

Synapse serves `/.well-known/matrix/client` automatically, derived from `public_baseurl`, and `/.well-known/matrix/server` because `serve_server_wellknown` is on. Both are proxied through the `@matrix` matcher.

The third, `/.well-known/element/element.json`, is served by Caddy as a literal string and is meant to tell Element X which Element Call deployment to use:

```json
{"call":{"widget_url":"https://element-call.example.com"}}
```

**It does not work.** The format is documented in [element-meta#2441](https://github.com/element-hq/element-meta/issues/2441), but neither the element-x-android source nor the element-x-ios source contains any reference to that file. Both resolve their Element Call URL from a bundled asset and a per-device override, and nothing else. The document is served because it costs one line and might be honoured by some future build. Do not spend an evening debugging why it did not configure anything.

## References

- Standing the stack up, and the config files these clients read: [01-deployment.md](01-deployment.md)
- Accounts, rooms, and access control: [02-operations.md](02-operations.md)
