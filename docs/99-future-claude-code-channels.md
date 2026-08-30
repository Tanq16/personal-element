# Claude Code interactive sessions over MCP channels

Not the current design. Agents in this deployment run as one-shot commands launched by the local `element-agent` binary. This file records the alternative that keeps a Claude Code session alive and pushes Matrix mentions into it, so it can be picked up if an agent ever needs continuity across mentions rather than a clean process per mention.

The trade is fixed: a channel gives an agent memory of the conversation and costs a machine that stays awake with a session running. A one-shot command gives isolation and costs the agent any memory of what it said last time.

## The channel contract

A channel is an MCP server that Claude Code spawns as a stdio subprocess and that pushes events into the running session. Only the transport is standard MCP; the method and schema are Claude Code extensions, so no other MCP client responds to them.

Server constructor:

```ts
const mcp = new Server(
  { name: 'matrix', version: '0.0.1' },
  {
    capabilities: {
      experimental: { 'claude/channel': {} },
      tools: {},
    },
    instructions: 'Messages arrive as <channel source="matrix" room_id="..." sender="...">. Reply with the reply tool, passing the room_id from the tag.',
  },
)
```

`capabilities.experimental['claude/channel']` is required and always `{}`. Its presence is what registers the notification listener. `capabilities.tools` is needed only for a two-way channel. The `instructions` string goes into Claude's system prompt.

Pushing an event:

```ts
await mcp.notification({
  method: 'notifications/claude/channel',
  params: {
    content: body,
    meta: { room_id, sender, event_id },
  },
})
```

`meta` keys must be letters, digits, and underscores only. Keys containing hyphens or other characters are silently dropped. Each entry becomes an attribute on the tag Claude sees:

```
<channel source="matrix" room_id="!AAAAAAAAAAAAAAAAAA" sender="@admin:element.example.com">
@agent_reviewer is that number right?
</channel>
```

`source` is set automatically from the server's configured name.

## Delivery semantics that shape the build

Claude Code does not acknowledge notifications. The `await` on `mcp.notification()` resolves when the message is written to the transport, not when Claude has processed it. If the session did not load the server as a channel, events are dropped with no error returned to the server. Delivery confirmation has to come from the reply tool reporting status back.

Events queue into the session and are processed in order. Several notifications arriving while Claude is busy are delivered together on the next turn and handled as a group. The documented answer for concurrent independent streams is to run separate sessions.

That batching is the constraint that reaches furthest into the design. One turn can be answering two mentions from two different rooms, so the reply tool takes `room_id` as an argument on every call. It must never acquire a notion of a current room or a last room, because the merged turn has neither.

## Loading it

Custom channels are not on the Anthropic-maintained allowlist during the research preview, so the session starts with:

```sh
claude --dangerously-load-development-channels server:matrix
```

Claude Code shows a full-screen warning listing the development channels being loaded, then asks for consent for the new server from `.mcp.json` on the first session in that project. A dim notice below the startup banner confirms registration: `Channels (experimental) messages from server:matrix inject directly in this session · restart without --dangerously-load-development-channels to stop`.

The flag skips the allowlist only. The `channelsEnabled` organization policy still applies.

## Context strategy: compaction, not clearing

A channel cannot carry a slash command. `content` is delivered as the body of the `<channel>` tag, which is context for Claude to react to rather than a line typed at the prompt, so sending `/clear` between mentions puts the literal text into the session and clears nothing.

Auto-compaction is the mechanism that keeps a long-lived session alive:

- It summarizes older history rather than discarding it, so it bounds context size and does not isolate one mention from the next.
- It fires as the context window fills, around 967K tokens by default. Set `CLAUDE_CODE_AUTO_COMPACT_WINDOW` to move that threshold.
- `DISABLE_AUTO_COMPACT` turns it off for one session and `autoCompactEnabled` turns it off in settings. Whichever of the two turns it off, the other cannot turn it back on.
- What survives a compaction is steerable from the agent's own `CLAUDE.md`:

```markdown
# Compact instructions

When you are using compact, please focus on the questions asked in the channel and the answers given, and drop tool output.
```

Bleed between mentions is inherent to this design. For a chat agent that is closer to a feature than a defect, since the agent remembers what the room discussed, but it means the same question asked twice does not get an independent answer the second time.

## Where the processes live

Three processes on the machine running the agent:

1. The `element-agent` client daemon, holding the connection to the server on the VPS and listening on loopback.
2. The `claude` session, launched inside that agent's directory under `~/.config/element-agent/<name>/` so it picks up that directory's `CLAUDE.md`, `.claude/skills/`, and `.mcp.json`.
3. The channel server, spawned by Claude Code as a stdio child from the `.mcp.json` in that directory.

The channel server long-polls the daemon on loopback and posts replies back to it, which keeps the daemon the only process that talks to the VPS. Claude Code spawns the channel server locally over stdio, so it can never be the appservice endpoint itself.

An agent on this path exists only while its session is running. The daemon knows whether the channel server has polled recently, so the server needs a liveness signal into the room when an agent goes dark, or mentions land nowhere with no explanation.

## Sender gating and permission relay

The channels documentation says to gate inbound messages on the sender to prevent prompt injection. That gate cannot be a sender allowlist in this deployment, because any room member being able to mention an agent is the entire feature. Every inbound message is therefore untrusted, which sets two rules:

- No Matrix credential goes into the session's environment. Reads and sends go through the daemon to the server, which holds the appservice `as_token` and performs them.
- Permission relay stays off. Declaring `capabilities.experimental['claude/channel/permission']` lets anyone who can reply through the channel approve tool use in the session, and here that is anyone in the room.

For reference if the trust model ever changes, relay works by declaring that capability, handling `notifications/claude/channel/permission_request`, and replying with `notifications/claude/channel/permission` carrying `request_id` and `behavior` set to `allow` or `deny`.

## Cost if it is built

About 150 lines of TypeScript for the channel server and about 80 lines of Go in the daemon to hold the per-agent queue and the loopback endpoints, on top of the exec path that already exists.

## References

- Channels reference: https://code.claude.com/docs/en/channels-reference
- Channels overview and research preview terms: https://code.claude.com/docs/en/channels
- Compaction settings: https://code.claude.com/docs/en/costs
- MCP capability negotiation, including the `experimental` field: https://modelcontextprotocol.io/specification/2025-06-18/basic/lifecycle
