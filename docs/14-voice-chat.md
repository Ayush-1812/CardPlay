# In-game voice chat

Players in the same room can talk to each other. Voice is opt-in, it never
starts on its own, and nothing is recorded or stored.

## How it works

Audio travels **directly between players** over WebRTC. The API server never
receives it. What the server does is:

1. issue the ICE configuration (STUN, and a TURN credential that expires),
2. relay the negotiation between two players it has checked are in the same
   room's voice channel,
3. tell the channel when the running game opens or closes voice.

Signalling rides the WebSocket the room already uses, so voice inherits the
session cookie, the origin check and the room authorization the rest of the
game has. There is no new endpoint exposed to the internet except
`GET /api/v1/voice/ice`, which requires a verified session.

### Topology: a mesh, and when to change it

Every participant connects to every other one. For a five-seat room that is
at most four outgoing streams per player — about 120 kbit/s up on Opus,
which a phone handles. The registry refuses a channel past
`voice.MaxParticipants` (8), because a mesh costs bandwidth with the square
of the participants.

If rooms ever grow past that, the replacement is an SFU (LiveKit, mediasoup,
Janus): one upstream per player, the server forwards. That is also what
would make the game policy enforceable rather than cooperative — see below.

```
player A ──signalling──▶ CardPlay API ──signalling──▶ player B
    └──────────────── audio (direct, or via TURN) ──────────┘
```

## What the server checks

Every signalling frame is validated against state the server owns, never
against anything the client says about itself:

| Check | Refusal |
|---|---|
| The player is a member of the room (from the database) | `NOT_FOUND` |
| The sender is in that room's voice channel | `VOICE_NOT_JOINED` |
| The recipient is in the **same** channel | `VOICE_NO_PEER` |
| The kind is `offer`, `answer`, `candidate` or `bye` | `INVALID_REQUEST` |
| The recipient id is well-formed and the payload is under 12 KiB | `INVALID_REQUEST` |
| The running game allows speech right now | `VOICE_CLOSED` |
| The channel is not full | `VOICE_FULL` |

The description itself is never parsed; it is relayed unchanged. Signalling
has its own rate budget (300 frames per 10 seconds) because ICE trickles
dozens of candidates in a few seconds and would otherwise trip the
conversational limit and close the socket mid-call.

A voice membership belongs to the socket that created it, so a second tab
taking over does not let the first tab's close remove the player. A socket
that closes for any reason leaves the channel.

## Game policy, and the limits of enforcing it

Games express a rule through `game.VoicePolicy`, the voice twin of
`game.ChatPolicy`. **Trump** closes voice for exactly the window it closes
text chat: from the deal of five until the trump suit is named, delegation
included (spec rule 20). The trump is chosen from cards nobody else may see,
and speech would carry the same information text would.

What the server does when a game closes voice:

1. **Refuses to relay signalling** — no new connection can be negotiated
   while the phase lasts.
2. **Pushes `voice.policy` to the channel** the moment the match state
   changes, without the client asking.
3. The client then **disables its microphone track and closes its peer
   connections**, so there is nothing left that could carry audio. The
   browser test asserts the connections are gone and that they come back
   after the suit is named.

**The limit, stated plainly:** in a peer-to-peer mesh the server cannot
*prevent* audio between two already-connected peers. Steps 1 and 2 are
enforcement; step 3 is the honest client complying. A player running a
modified client could keep a connection open from before the restricted
phase and keep talking, and the server would not see it. Closing the
connections narrows the window to "a connection that existed before the
phase started", but it does not close it.

Enforcing this properly requires the audio to pass through a server — an
SFU, which can simply stop forwarding a participant's track. That is a
deliberate trade: a mesh costs nothing to run and suits a card game among
friends, where the realistic threat is a player talking out of turn, not
one patching their browser. If the threat model changes, move to an SFU and
the policy becomes a server-side mute.

Text chat is unaffected and remains fully enforced, because messages do pass
through the server.

## Privacy

- The microphone is opened only after an explicit **Join voice**, and the
  bar says so before anything is requested.
- Audio is never recorded, stored or forwarded by the server. The
  participant registry is in memory and holds a user id and three booleans
  (muted, deafened, speaking); it is emptied when the socket closes.
- Speaking detection runs in the browser on the player's own stream. Only
  the resulting boolean is published, throttled.
- Leaving voice, closing the tab or navigating away stops the tracks, which
  is what turns the browser's recording indicator off.
- TURN credentials are per player and short-lived. The shared secret never
  leaves the server.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `VOICE_ENABLED` | `1` | `0` disables voice entirely: signalling is refused and the client hides the controls. |
| `VOICE_STUN_URLS` | `stun:stun.l.google.com:19302` | Comma-separated STUN URLs. Replace to avoid depending on a third party. |
| `VOICE_TURN_URLS` | empty | Comma-separated TURN/TURNS URLs. Without them, players behind strict NAT cannot connect. |
| `VOICE_TURN_SECRET` | empty | Shared secret for coturn's REST scheme. Required with TURN URLs, at least 16 characters. |
| `VOICE_TURN_TTL` | `10m` | How long an issued TURN credential lasts (1m–1h). |

Invalid settings fail at startup rather than in a player's browser: a TURN
URL without a secret, a malformed scheme or a TTL out of range all refuse to
boot.

## Setting up TURN (coturn)

Voice works without TURN on ordinary home networks. It is mobile networks,
carrier-grade NAT and office firewalls that need a relay. One small VM is
enough for a game this size.

```sh
sudo apt install coturn
sudo systemctl enable coturn
```

`/etc/turnserver.conf`:

```conf
listening-port=3478
tls-listening-port=5349
fingerprint
# The REST scheme: coturn recomputes the HMAC instead of storing passwords.
use-auth-secret
static-auth-secret=THE_SAME_VALUE_AS_VOICE_TURN_SECRET
realm=turn.example.com
# Certificates, so turns: works where UDP is blocked.
cert=/etc/letsencrypt/live/turn.example.com/fullchain.pem
pkey=/etc/letsencrypt/live/turn.example.com/privkey.pem
# Do not relay to private ranges.
no-multicast-peers
denied-peer-ip=10.0.0.0-10.255.255.255
denied-peer-ip=172.16.0.0-172.31.255.255
denied-peer-ip=192.168.0.0-192.168.255.255
total-quota=100
```

Open 3478/udp, 3478/tcp, 5349/tcp and the relay range (49152–65535/udp).
Then set, on the API:

```sh
VOICE_TURN_URLS=turns:turn.example.com:5349?transport=tcp,turn:turn.example.com:3478?transport=udp
VOICE_TURN_SECRET=THE_SAME_VALUE_AS_static-auth-secret
```

Hosted alternatives (Twilio, Cloudflare, Metered) work the same way if they
support the REST scheme; otherwise set the URLs and leave the secret empty
and the entry will carry no credential, which only suits open relays.

### On Render

`render.yaml` declares `VOICE_ENABLED`, `VOICE_TURN_URLS` and
`VOICE_TURN_SECRET` with `sync: false`: set the last two in the dashboard so
the secret is never in the repository. Voice needs no extra ports on the API
itself — the relay is a separate host.

### Browser requirements

Microphone access needs a secure context: HTTPS, or `localhost` in
development. The deployed client is HTTPS, so this is already satisfied.

## What is tested

Go (`internal/voice`, `internal/game/trump`, `internal/server`):

- A TURN username names the player it was issued to, expires, and is the
  HMAC the relay will recompute; two players never share a credential, and
  the secret never appears in what is sent.
- Channels do not leak between rooms; a stranger's state is refused; the
  mesh is bounded; a rejoin is not a new participant; a stale tab's socket
  cannot remove a newer tab's membership.
- Trump's gate is closed during selection with a reason, open in play.
- Against a live server and database: a stranger cannot join a channel, an
  un-joined player cannot signal, two players in a room reach each other
  with the description unchanged and attributed, a player in another room
  cannot be addressed even when named correctly, malformed frames are
  refused, and leaving tells the channel.

Browser (`scripts/e2e/tests/voice.spec.ts`), four isolated sessions:

- Four players join one channel and each ends up **receiving audio on three
  peer connections** (a remote track reports muted until packets arrive).
- Mute is seen by the others in words; volume is per player and reaches the
  audio element.
- Push-to-talk is verified from the far side: another player sees speech
  start when held and stop when released.
- Trump's restricted phase: every client is told without asking, the peer
  connections are gone while it lasts, and the mesh rebuilds after the suit
  is named.
- A dropped socket rejoins the channel; leaving releases the connections; a
  refused microphone leaves the game fully playable; two rooms' channels
  never meet.

## What could not be tested here

- **A physical microphone.** This development machine's operating system
  refuses microphone access to browsers — Chrome and Edge both return
  `NotAllowedError` even with Chromium's fake devices present. The browser
  tests therefore answer `getUserMedia` in the page with a synthesised
  440 Hz track. It is a real `MediaStreamTrack` that WebRTC encodes and
  sends, so everything from `getUserMedia` onward is the production path;
  what is untested is the hardware behind it and the constraints
  (`echoCancellation`, `noiseSuppression`, `autoGainControl`), which the
  browser applies to a real device only.
- **A forced TURN relay.** No TURN server runs in the test environment, so
  the relay path is exercised only by the credential tests. Before relying
  on voice over mobile networks, deploy coturn and confirm with
  `iceTransportPolicy: "relay"` in the browser console, or Trickle ICE.
- **Cross-device audio quality** (echo, latency, gain) — needs two real
  devices in a room.
