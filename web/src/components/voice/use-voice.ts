"use client";

// In-game voice chat, from the client's side.
//
// One peer connection per other player in the channel (a mesh): CardPlay's
// rooms seat at most five, so nobody is sending more than four streams. The
// server relays the negotiation only, which means it can refuse to let a
// connection form while a game closes voice, but it cannot inspect or stop
// audio already flowing between two peers. See docs/14-voice-chat.md.
//
// The microphone is opened only when the player asks, and released when they
// leave, when the component unmounts, and when the tab goes away.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  fetchIce,
  IceConfig,
  isSpeaking,
  MIC_CONSTRAINTS,
  micErrorMessage,
  shouldOffer,
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

type Peer = {
  pc: RTCPeerConnection;
  audio: HTMLAudioElement;
  // Candidates that arrived before the description they belong to.
  pending: RTCIceCandidateInit[];
  settled: boolean;
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

  const connectToRef = useRef<(id: string) => Promise<void>>(async () => {});
  const streamRef = useRef<MediaStream | null>(null);
  const peersRef = useRef(new Map<string, Peer>());
  const iceRef = useRef<{ config: IceConfig; expires: number } | null>(null);
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

  // ---- peer connections --------------------------------------------
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
    peer.pc.close();
    peer.audio.srcObject = null;
    peer.audio.remove();
  }, []);

  const ice = useCallback(async (): Promise<IceConfig> => {
    const held = iceRef.current;
    if (held && held.expires > Date.now()) return held.config;
    const config = await fetchIce();
    iceRef.current = {
      config,
      // Renew a little early so a connection never starts with a credential
      // that expires mid-handshake.
      expires: Date.now() + Math.max(30, config.ttl_seconds - 60) * 1000,
    };
    setRelayAvailable(config.relay_available);
    return config;
  }, []);

  const peerFor = useCallback(
    async (id: string): Promise<Peer> => {
      const held = peersRef.current.get(id);
      if (held) return held;
      const config = await ice();
      const pc = new RTCPeerConnection({ iceServers: config.ice_servers });
      const audio = document.createElement("audio");
      audio.autoplay = true;
      audio.muted = transmitRef.current.deafened;
      audio.volume = volumesRef.current[id] ?? 1;
      audio.setAttribute("data-voice-peer", id);
      // Kept out of the layout: it exists to play, not to be seen.
      audio.style.display = "none";
      document.body.append(audio);
      const peer: Peer = { pc, audio, pending: [], settled: false };
      peersRef.current.set(id, peer);

      const stream = streamRef.current;
      if (stream)
        for (const track of stream.getAudioTracks()) pc.addTrack(track, stream);

      pc.onicecandidate = (e) => {
        if (e.candidate) void signal(id, "candidate", e.candidate.toJSON());
      };
      pc.ontrack = (e) => {
        audio.srcObject = e.streams[0] ?? new MediaStream([e.track]);
        void audio.play().catch(() => setBlockedAudio(true));
      };
      pc.onconnectionstatechange = () => {
        if (pc.connectionState === "failed") {
          // A failed connection is retried from scratch; ICE restarts are
          // not worth the complexity for a four-player mesh.
          dropPeer(id);
          if (shouldOffer(selfID, id)) void connectToRef.current(id);
        }
      };
      return peer;
    },
    [ice, signal, dropPeer, selfID],
  );

  // connectTo and peerFor refer to each other -- a failed connection is
  // rebuilt by offering again -- so the offer side is reached through a ref.
  const connectTo = useCallback((id: string) => connectToRef.current(id), []);
  useEffect(() => {
    connectToRef.current = async (id: string) => {
      if (!policyRef.current.open) return;
      const peer = await peerFor(id);
      if (peer.pc.signalingState !== "stable") return;
      const offer = await peer.pc.createOffer();
      await peer.pc.setLocalDescription(offer);
      await signal(id, "offer", offer);
    };
  }, [peerFor, signal]);

  const onSignal = useCallback(
    async (from: string, kind: SignalKind, data: unknown) => {
      if (kind === "bye") {
        dropPeer(from);
        return;
      }
      const peer = await peerFor(from);
      if (kind === "offer") {
        await peer.pc.setRemoteDescription(data as RTCSessionDescriptionInit);
        peer.settled = true;
        for (const c of peer.pending.splice(0))
          await peer.pc.addIceCandidate(c);
        const answer = await peer.pc.createAnswer();
        await peer.pc.setLocalDescription(answer);
        await signal(from, "answer", answer);
        return;
      }
      if (kind === "answer") {
        if (peer.pc.signalingState !== "have-local-offer") return;
        await peer.pc.setRemoteDescription(data as RTCSessionDescriptionInit);
        peer.settled = true;
        for (const c of peer.pending.splice(0))
          await peer.pc.addIceCandidate(c);
        return;
      }
      const candidate = data as RTCIceCandidateInit;
      if (!peer.settled) {
        peer.pending.push(candidate);
        return;
      }
      await peer.pc.addIceCandidate(candidate).catch(() => {});
    },
    [peerFor, dropPeer, signal],
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
      const config = await ice();
      if (!config.enabled) {
        setStatus("unavailable");
        setError("Voice chat is switched off on this server.");
        return;
      }
      stream = await navigator.mediaDevices.getUserMedia(MIC_CONSTRAINTS);
    } catch (e) {
      setStatus("error");
      setError(micErrorMessage(e));
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
  useEffect(() => {
    if (!liveRef.current) return;
    const here = new Set(participants.map((p) => p.user_id));
    for (const id of [...peersRef.current.keys()])
      if (!here.has(id)) dropPeer(id);
    if (!policy.open) return;
    for (const p of participants) {
      if (p.user_id === selfID || peersRef.current.has(p.user_id)) continue;
      if (shouldOffer(selfID, p.user_id)) void connectTo(p.user_id);
    }
  }, [participants, policy.open, selfID, dropPeer, connectTo]);

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

  // Browsers refuse to play audio until the page has been interacted with;
  // this button is that interaction.
  const enableAudio = useCallback(() => {
    setBlockedAudio(false);
    for (const peer of peersRef.current.values())
      void peer.audio.play().catch(() => setBlockedAudio(true));
  }, []);

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
