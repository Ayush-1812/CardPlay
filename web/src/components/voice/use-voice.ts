"use client";

// In-game voice chat, from the client's side.
//
// One peer connection per other player in the channel (a mesh): CardPlay's
// rooms seat at most six, so nobody is sending more than five streams. The
// server relays the negotiation only, which means it can refuse to let a
// connection form while a game closes voice, but it cannot inspect or stop
// audio already flowing between two peers. See docs/14-voice-chat.md.
//
// Three things make the mesh hold up with four or five people, where a
// naive version drops a pair here and there:
//
//  1. A peer connection is created synchronously. Anything awaited between
//     "is there a connection for this player?" and "here is one" lets two
//     callers -- an arriving offer and our own connect -- each build one,
//     and the loser's answers and candidates then arrive at an object
//     nobody is holding. That pair stays silent while the rest work.
//  2. Negotiation is the perfect-negotiation pattern, so two offers that
//     cross in flight resolve instead of deadlocking.
//  3. A watchdog reconciles the mesh every couple of seconds: a missing
//     connection is created, and one that has not reached "connected" is
//     rebuilt. Nothing is left to a single lost message.
//
// The microphone is opened only when the player asks, and released when
// they leave, when the component unmounts, and when the tab goes away.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  fetchIce,
  IceConfig,
  isSpeaking,
  MIC_CONSTRAINTS,
  micErrorMessage,
  SignalKind,
  VoiceParticipant,
  VoicePolicy,
} from "../../lib/voice";

export type Reply = {
  type: string;
  payload?: { code?: string; message?: string };
};
export type VoiceSend = (frame: Record<string, unknown>) => Promise<Reply>;
export type VoiceFrame = { type: string; payload?: unknown };

export type VoiceStatus =
  "off" | "asking" | "joining" | "live" | "error" | "unavailable";

export type VoiceMode = "open" | "ptt";

// How long a connection may go without reaching "connected" before it is
// torn down and tried again. Long enough for ICE on a slow network, short
// enough that nobody sits in silence wondering.
const STALL_MS = 9000;
// How long a connected link may carry nothing before it is rebuilt. A link
// can be "connected" and silent -- which is exactly what a player means by
// "I cannot hear them" -- so the watchdog counts packets, not states.
const SILENT_MS = 8000;
// How often the mesh is reconciled.
const WATCHDOG_MS = 2000;

type Peer = {
  pc: RTCPeerConnection;
  audio: HTMLAudioElement;
  // polite yields when two offers cross; the higher user id is polite, so
  // exactly one side of every pair gives way.
  polite: boolean;
  makingOffer: boolean;
  ignoreOffer: boolean;
  // Candidates that arrived before the description they belong to.
  pending: RTCIceCandidateInit[];
  // When this connection was created or last reached "connected", which is
  // what the watchdog measures a stall against.
  since: number;
  // The last packet count seen on this link, and when it last grew.
  packets: number;
  heard: number;
  // The browser refused to play this peer's audio until a gesture.
  blocked: boolean;
};

type Options = {
  roomID: string | null;
  selfID: string;
  connection: string;
  send: VoiceSend | null;
  // register hands the dashboard a callback for voice frames; it returns an
  // unsubscribe function.
  register: (fn: (frame: VoiceFrame) => void) => () => void;
};

export function useVoice({
  roomID,
  selfID,
  connection,
  send,
  register,
}: Options) {
  const [status, setStatus] = useState<VoiceStatus>("off");
  const [participants, setParticipants] = useState<VoiceParticipant[]>([]);
  const [policy, setPolicy] = useState<VoicePolicy>({ open: true });
  const [error, setError] = useState("");
  const [muted, setMuted] = useState(false);
  const [deafened, setDeafened] = useState(false);
  const [mode, setMode] = useState<VoiceMode>("open");
  const [talking, setTalking] = useState(false);
  const [speaking, setSpeaking] = useState(false);
  const [blockedAudio, setBlockedAudio] = useState(false);
  const [relayAvailable, setRelayAvailable] = useState(false);
  const [volumes, setVolumesState] = useState<Record<string, number>>({});
  // What each peer connection is doing, so a player who cannot hear someone
  // can see whether that link is still coming up.
  const [links, setLinks] = useState<Record<string, RTCPeerConnectionState>>(
    {},
  );

  const connectToRef = useRef<(id: string) => void>(() => {});
  const streamRef = useRef<MediaStream | null>(null);
  const peersRef = useRef(new Map<string, Peer>());
  const iceRef = useRef<{ config: IceConfig; expires: number } | null>(null);
  const icePending = useRef<Promise<IceConfig> | null>(null);
  const joinedRef = useRef(false);
  const sendRef = useRef<VoiceSend | null>(send);
  const roomRef = useRef<string | null>(roomID);
  const volumesRef = useRef<Record<string, number>>({});
  const publishedRef = useRef({
    muted: false,
    deafened: false,
    speaking: false,
  });
  const audioCtxRef = useRef<AudioContext | null>(null);
  const liveRef = useRef(false);
  const policyRef = useRef<VoicePolicy>({ open: true });
  const participantsRef = useRef<VoiceParticipant[]>([]);
  const selfRef = useRef(selfID);
  const transmitRef = useRef({
    muted: false,
    deafened: false,
    mode: "open" as VoiceMode,
    talking: false,
  });

  // The callbacks below outlive any one render, so what they need is kept
  // in refs and synced after every render rather than captured.
  useEffect(() => {
    sendRef.current = send;
    roomRef.current = roomID;
    policyRef.current = policy;
    participantsRef.current = participants;
    selfRef.current = selfID;
    transmitRef.current = { muted, deafened, mode, talking };
  });

  // ---- transmission ------------------------------------------------
  // One rule decides whether the microphone track is live, so the game
  // policy, mute, deafen and push-to-talk can never disagree.
  const applyTransmit = useCallback(() => {
    const stream = streamRef.current;
    if (!stream) return;
    const t = transmitRef.current;
    const allowed =
      policyRef.current.open &&
      !t.muted &&
      !t.deafened &&
      (t.mode === "open" || t.talking);
    for (const track of stream.getAudioTracks()) track.enabled = allowed;
  }, []);

  useEffect(applyTransmit, [
    muted,
    deafened,
    mode,
    talking,
    policy,
    applyTransmit,
  ]);

  // Deafening also silences everyone else's audio elements.
  useEffect(() => {
    for (const peer of peersRef.current.values()) peer.audio.muted = deafened;
  }, [deafened]);

  // ---- publishing our own state ------------------------------------
  const publish = useCallback(
    (next: { muted: boolean; deafened: boolean; speaking: boolean }) => {
      const prev = publishedRef.current;
      if (
        prev.muted === next.muted &&
        prev.deafened === next.deafened &&
        prev.speaking === next.speaking
      )
        return;
      publishedRef.current = next;
      const room = roomRef.current;
      if (!room || !joinedRef.current) return;
      void sendRef.current?.({
        type: "voice.state",
        room_id: room,
        payload: next,
      });
    },
    [],
  );

  useEffect(() => {
    if (!liveRef.current) return;
    publish({ muted, deafened, speaking });
  }, [muted, deafened, speaking, publish]);

  // ---- signalling ---------------------------------------------------
  const signal = useCallback(
    async (to: string, kind: SignalKind, data: unknown) => {
      const room = roomRef.current;
      if (!room) return;
      const reply = await sendRef.current?.({
        type: "voice.signal",
        room_id: room,
        payload: { to, kind, data },
      });
      // A refusal is not fatal to the game: the policy may have closed voice
      // between this frame leaving and arriving.
      if (
        reply &&
        reply.type === "error" &&
        reply.payload?.code === "VOICE_CLOSED"
      )
        setPolicy({ open: false, reason: reply.payload.message });
    },
    [],
  );

  const dropPeer = useCallback((id: string) => {
    const peer = peersRef.current.get(id);
    if (!peer) return;
    peersRef.current.delete(id);
    peer.pc.onicecandidate = null;
    peer.pc.ontrack = null;
    peer.pc.onconnectionstatechange = null;
    peer.pc.onnegotiationneeded = null;
    peer.pc.close();
    peer.audio.srcObject = null;
    peer.audio.remove();
    setLinks((held) => {
      const next = { ...held };
      delete next[id];
      return next;
    });
  }, []);

  // ice fetches the servers once and shares one request: several peers
  // coming up together must not each ask for their own credential.
  const ice = useCallback(async (): Promise<IceConfig> => {
    const held = iceRef.current;
    if (held && held.expires > Date.now()) return held.config;
    if (!icePending.current) {
      icePending.current = fetchIce()
        .then((config) => {
          iceRef.current = {
            config,
            // Renewed early, so a connection never starts with a credential
            // about to expire.
            expires: Date.now() + Math.max(30, config.ttl_seconds - 60) * 1000,
          };
          setRelayAvailable(config.relay_available);
          return config;
        })
        .finally(() => {
          icePending.current = null;
        });
    }
    return icePending.current;
  }, []);

  // playPeer starts a peer's audio, remembering a browser that refused.
  const playPeer = useCallback((peer: Peer) => {
    void peer.audio
      .play()
      .then(() => {
        peer.blocked = false;
      })
      .catch(() => {
        peer.blocked = true;
        setBlockedAudio(true);
      });
  }, []);

  // peerFor returns this player's connection to another, creating it if
  // there is none. It is deliberately synchronous: nothing may be awaited
  // between the lookup and the insert, or two callers will each create one
  // and the loser's answers will arrive at a connection nobody holds.
  const peerFor = useCallback(
    (id: string): Peer | null => {
      const held = peersRef.current.get(id);
      if (held) return held;
      const config = iceRef.current?.config;
      if (!config) {
        // No servers yet. Fetch them; the watchdog builds this connection
        // on its next pass.
        void ice();
        return null;
      }
      const pc = new RTCPeerConnection({ iceServers: config.ice_servers });
      const audio = document.createElement("audio");
      audio.autoplay = true;
      audio.muted = transmitRef.current.deafened;
      audio.volume = volumesRef.current[id] ?? 1;
      audio.setAttribute("data-voice-peer", id);
      // Kept out of the layout: it exists to play, not to be seen.
      audio.style.display = "none";
      document.body.append(audio);
      const peer: Peer = {
        pc,
        audio,
        // The higher user id is polite and gives way when offers cross, so
        // both sides agree who yields without asking anyone.
        polite: selfRef.current > id,
        makingOffer: false,
        ignoreOffer: false,
        pending: [],
        since: Date.now(),
        packets: 0,
        heard: Date.now(),
        blocked: false,
      };
      peersRef.current.set(id, peer);

      const stream = streamRef.current;
      if (stream)
        for (const track of stream.getAudioTracks()) pc.addTrack(track, stream);

      pc.onicecandidate = (e) => {
        if (e.candidate) void signal(id, "candidate", e.candidate.toJSON());
      };
      pc.ontrack = (e) => {
        audio.srcObject = e.streams[0] ?? new MediaStream([e.track]);
        playPeer(peer);
      };
      pc.onnegotiationneeded = () => {
        void (async () => {
          try {
            peer.makingOffer = true;
            await pc.setLocalDescription();
            await signal(id, "offer", pc.localDescription);
          } catch {
            // The watchdog rebuilds anything that does not come up.
          } finally {
            peer.makingOffer = false;
          }
        })();
      };
      pc.onconnectionstatechange = () => {
        const state = pc.connectionState;
        setLinks((before) => ({ ...before, [id]: state }));
        if (state === "connected") {
          peer.since = Date.now();
          // A connection that comes up after the browser refused to play
          // its audio gets another go.
          if (peer.blocked) playPeer(peer);
        }
      };
      return peer;
    },
    [ice, signal, playPeer],
  );

  // connectTo makes sure a connection to this player exists. Creating it
  // fires negotiationneeded, which is what sends the offer.
  const connectTo = useCallback(
    (id: string) => {
      if (!policyRef.current.open || id === selfRef.current) return;
      peerFor(id);
    },
    [peerFor],
  );
  useEffect(() => {
    connectToRef.current = connectTo;
  }, [connectTo]);

  // onSignal applies one negotiation frame. Two offers that cross are
  // resolved rather than deadlocked: the impolite side ignores the other's
  // offer, the polite side rolls back and answers it.
  const onSignal = useCallback(
    async (from: string, kind: SignalKind, data: unknown) => {
      if (kind === "bye") {
        dropPeer(from);
        return;
      }
      let peer = peersRef.current.get(from);
      if (!peer) {
        // An offer can arrive before the servers have been fetched.
        if (!iceRef.current?.config) await ice();
        peer = peerFor(from) ?? undefined;
      }
      if (!peer) return;
      const pc = peer.pc;
      try {
        if (kind === "offer" || kind === "answer") {
          const description = data as RTCSessionDescriptionInit;
          const collision =
            description.type === "offer" &&
            (peer.makingOffer || pc.signalingState !== "stable");
          peer.ignoreOffer = !peer.polite && collision;
          if (peer.ignoreOffer) return;
          await pc.setRemoteDescription(description);
          for (const candidate of peer.pending.splice(0))
            await pc.addIceCandidate(candidate).catch(() => {});
          if (description.type === "offer") {
            await pc.setLocalDescription();
            await signal(from, "answer", pc.localDescription);
          }
          return;
        }
        const candidate = data as RTCIceCandidateInit;
        // Candidates that arrive before the description they belong to are
        // held rather than thrown away.
        if (!pc.remoteDescription) {
          peer.pending.push(candidate);
          return;
        }
        await pc.addIceCandidate(candidate);
      } catch {
        // Nothing here is worth interrupting a game for: the watchdog
        // rebuilds a connection that does not come up.
      }
    },
    [dropPeer, ice, peerFor, signal],
  );

  // ---- joining and leaving -----------------------------------------
  const teardown = useCallback(
    (reason: VoiceStatus) => {
      liveRef.current = false;
      joinedRef.current = false;
      for (const id of [...peersRef.current.keys()]) dropPeer(id);
      // Releasing the tracks is what turns the browser's recording
      // indicator off; closing the connections alone would not.
      streamRef.current?.getTracks().forEach((t) => t.stop());
      streamRef.current = null;
      void audioCtxRef.current?.close().catch(() => {});
      audioCtxRef.current = null;
      publishedRef.current = { muted: false, deafened: false, speaking: false };
      setParticipants([]);
      setLinks({});
      setSpeaking(false);
      setTalking(false);
      setBlockedAudio(false);
      setStatus(reason);
    },
    [dropPeer],
  );

  const leave = useCallback(() => {
    const room = roomRef.current;
    if (room && joinedRef.current) {
      for (const id of peersRef.current.keys()) void signal(id, "bye", {});
      void sendRef.current?.({ type: "voice.leave", room_id: room });
    }
    teardown("off");
  }, [signal, teardown]);

  // watchLevel lights the speaking indicator from our own microphone. It
  // reads levels in the browser and never sends audio anywhere.
  const watchLevel = useCallback((stream: MediaStream) => {
    type WindowWithAudio = Window & {
      webkitAudioContext?: typeof AudioContext;
    };
    const Ctor =
      window.AudioContext ?? (window as WindowWithAudio).webkitAudioContext;
    if (!Ctor) return;
    const ctx = new Ctor();
    audioCtxRef.current = ctx;
    const source = ctx.createMediaStreamSource(stream);
    const analyser = ctx.createAnalyser();
    analyser.fftSize = 512;
    source.connect(analyser);
    const samples = new Float32Array(analyser.fftSize);
    let last = 0;
    const tick = () => {
      if (ctx.state === "closed") return;
      analyser.getFloatTimeDomainData(samples);
      const on =
        stream.getAudioTracks().some((t) => t.enabled) && isSpeaking(samples);
      const now = Date.now();
      // Throttled: an indicator, not a stream of state updates.
      if (now - last > 300) {
        last = now;
        setSpeaking((was) => (was === on ? was : on));
      }
      requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  }, []);

  const join = useCallback(async () => {
    const room = roomRef.current;
    if (!room || !sendRef.current || joinedRef.current) return;
    setError("");
    setStatus("asking");
    let stream: MediaStream;
    try {
      // Fetched before any connection is built, so peers can be created
      // without awaiting anything.
      const config = await ice();
      if (!config.enabled) {
        setStatus("unavailable");
        setError("Voice chat is switched off on this server.");
        return;
      }
      stream = await navigator.mediaDevices.getUserMedia(MIC_CONSTRAINTS);
    } catch (e) {
      setStatus("error");
      setError(await micErrorMessage(e));
      return;
    }
    streamRef.current = stream;
    setStatus("joining");
    const reply = await sendRef.current({ type: "voice.join", room_id: room });
    if (reply.type !== "voice.joined") {
      stream.getTracks().forEach((t) => t.stop());
      streamRef.current = null;
      setStatus(
        reply.payload?.code === "VOICE_DISABLED" ? "unavailable" : "error",
      );
      setError(reply.payload?.message ?? "Voice is unavailable right now.");
      return;
    }
    joinedRef.current = true;
    liveRef.current = true;
    applyTransmit();
    watchLevel(stream);
    setStatus("live");
  }, [ice, applyTransmit, watchLevel]);

  // ---- frames from the server --------------------------------------
  useEffect(() => {
    return register((frame) => {
      const payload = frame.payload as
        | {
            self?: string;
            participants?: VoiceParticipant[];
            policy?: VoicePolicy;
            from?: string;
            kind?: SignalKind;
            data?: unknown;
            open?: boolean;
            reason?: string;
          }
        | undefined;
      if (!payload) return;
      if (frame.type === "voice.joined") {
        if (payload.policy) setPolicy(payload.policy);
        if (payload.participants) setParticipants(payload.participants);
        return;
      }
      if (frame.type === "voice.participants" && payload.participants) {
        setParticipants(payload.participants);
        return;
      }
      if (frame.type === "voice.policy") {
        setPolicy({ open: payload.open ?? true, reason: payload.reason });
        return;
      }
      if (frame.type === "voice.signal" && payload.from && payload.kind) {
        void onSignal(payload.from, payload.kind, payload.data).catch(() => {});
      }
    });
  }, [register, onSignal]);

  // Connect to everyone the server says is here; drop anyone who left.
  // Both sides of a pair build their connection rather than one waiting to
  // be called: a crossed pair of offers is resolved, a missing one is not.
  useEffect(() => {
    if (!liveRef.current) return;
    const here = new Set(participants.map((p) => p.user_id));
    for (const id of [...peersRef.current.keys()])
      if (!here.has(id)) dropPeer(id);
    if (!policy.open) return;
    for (const p of participants) {
      if (p.user_id === selfID) continue;
      connectTo(p.user_id);
    }
  }, [participants, policy.open, selfID, dropPeer, connectTo]);

  // The watchdog. Signalling can be lost, an offer can cross a reconnect,
  // a connection can stall behind a firewall; none of it should leave two
  // players unable to hear each other with nothing happening about it.
  useEffect(() => {
    if (status !== "live") return;
    const timer = setInterval(() => {
      void (async () => {
        if (!liveRef.current || !policyRef.current.open) return;
        const now = Date.now();
        for (const p of participantsRef.current) {
          if (p.user_id === selfRef.current) continue;
          const peer = peersRef.current.get(p.user_id);
          if (!peer) {
            connectToRef.current(p.user_id);
            continue;
          }
          const state = peer.pc.connectionState;
          if (state !== "connected") {
            if (state === "failed" || now - peer.since > STALL_MS) {
              // Start again rather than nurse a connection that has not
              // come up: creating one offers again.
              dropPeer(p.user_id);
              connectToRef.current(p.user_id);
            }
            continue;
          }
          peer.since = now;
          // Connected is not the same as audible. Count what has actually
          // arrived; a link that has gone quiet while the other player is
          // not muted is a link to rebuild.
          let received = 0;
          try {
            const stats = await peer.pc.getStats();
            stats.forEach((report) => {
              const r = report as {
                type: string;
                kind?: string;
                packetsReceived?: number;
              };
              if (r.type === "inbound-rtp" && r.kind !== "video")
                received += r.packetsReceived ?? 0;
            });
          } catch {
            continue;
          }
          if (received > peer.packets) {
            peer.packets = received;
            peer.heard = now;
            continue;
          }
          // A muted player legitimately sends very little; they are not a
          // broken link.
          if (p.muted) {
            peer.heard = now;
            continue;
          }
          if (now - peer.heard > SILENT_MS) {
            dropPeer(p.user_id);
            connectToRef.current(p.user_id);
          }
        }
      })();
    }, WATCHDOG_MS);
    return () => clearInterval(timer);
  }, [status, dropPeer]);

  // A closed phase stops transmission immediately and tears the mesh down,
  // so nothing is left connected that could carry audio while it lasts.
  useEffect(() => {
    applyTransmit();
    if (policy.open || !liveRef.current) return;
    for (const id of [...peersRef.current.keys()]) {
      void signal(id, "bye", {});
      dropPeer(id);
    }
  }, [policy.open, applyTransmit, dropPeer, signal]);

  // The socket dropping takes the channel with it: the server forgets the
  // membership when the connection closes, so rejoin once it is back.
  useEffect(() => {
    if (!liveRef.current) return;
    if (connection === "Connected" && !joinedRef.current) {
      void (async () => {
        const room = roomRef.current;
        if (!room || !sendRef.current) return;
        for (const id of [...peersRef.current.keys()]) dropPeer(id);
        const reply = await sendRef.current({
          type: "voice.join",
          room_id: room,
        });
        if (reply.type === "voice.joined") {
          joinedRef.current = true;
          setStatus("live");
        }
      })();
      return;
    }
    if (connection !== "Connected" && joinedRef.current) {
      joinedRef.current = false;
      setStatus("joining");
    }
  }, [connection, dropPeer]);

  // Leaving the room, or the page, releases the microphone.
  useEffect(() => {
    if (roomID) return;
    if (liveRef.current) teardown("off");
  }, [roomID, teardown]);

  useEffect(() => {
    const drop = () => {
      if (liveRef.current) teardown("off");
    };
    window.addEventListener("pagehide", drop);
    return () => {
      window.removeEventListener("pagehide", drop);
      drop();
    };
  }, [teardown]);

  // ---- controls -----------------------------------------------------
  const setVolume = useCallback((id: string, value: number) => {
    volumesRef.current = { ...volumesRef.current, [id]: value };
    setVolumesState(volumesRef.current);
    const peer = peersRef.current.get(id);
    if (peer) peer.audio.volume = value;
  }, []);

  // Browsers refuse to play audio until the page has been interacted with.
  // This is that interaction, and it retries every peer, not just the first
  // one that was refused.
  const enableAudio = useCallback(() => {
    let blocked = false;
    for (const peer of peersRef.current.values()) {
      if (!peer.audio.paused) continue;
      void peer.audio.play().catch(() => {
        peer.blocked = true;
        blocked = true;
      });
    }
    setBlockedAudio(blocked);
  }, []);

  // Any interaction with the page counts, so a player who taps a card gets
  // their table's audio without having to find the button.
  useEffect(() => {
    if (status !== "live" || !blockedAudio) return;
    const retry = () => enableAudio();
    document.addEventListener("pointerdown", retry);
    document.addEventListener("keydown", retry);
    return () => {
      document.removeEventListener("pointerdown", retry);
      document.removeEventListener("keydown", retry);
    };
  }, [status, blockedAudio, enableAudio]);

  return {
    status,
    participants,
    policy,
    error,
    muted,
    deafened,
    mode,
    speaking,
    talking,
    blockedAudio,
    relayAvailable,
    volumes,
    links,
    join,
    leave,
    toggleMute: () => setMuted((m) => !m),
    toggleDeafen: () => setDeafened((d) => !d),
    setMode,
    setTalking,
    setVolume,
    enableAudio,
  };
}
