"use client";

// The voice controls. Everything a player can do to their own microphone and
// to what they hear, in one bar that fits a phone and reads to a screen
// reader. Voice is opt-in: nothing opens the microphone until "Join voice".

import { useEffect, useRef } from "react";
import type { VoiceMode, VoiceStatus } from "./use-voice";
import type { VoiceParticipant, VoicePolicy } from "../../lib/voice";

// The key held for push-to-talk. V is away from the game's own controls and
// is not a browser shortcut.
const PTT_KEY = "v";

export type VoiceBarProps = {
  status: VoiceStatus;
  participants: VoiceParticipant[];
  policy: VoicePolicy;
  error: string;
  muted: boolean;
  deafened: boolean;
  mode: VoiceMode;
  speaking: boolean;
  talking: boolean;
  blockedAudio: boolean;
  relayAvailable: boolean;
  volumes: Record<string, number>;
  selfID: string;
  name: (userID: string) => string;
  join: () => void;
  leave: () => void;
  toggleMute: () => void;
  toggleDeafen: () => void;
  setMode: (mode: VoiceMode) => void;
  setTalking: (talking: boolean) => void;
  setVolume: (userID: string, value: number) => void;
  enableAudio: () => void;
};

export function VoiceBar({
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
  selfID,
  name,
  join,
  leave,
  toggleMute,
  toggleDeafen,
  setMode,
  setTalking,
  setVolume,
  enableAudio,
}: VoiceBarProps) {
  const live = status === "live";
  const held = useRef(false);

  // Push-to-talk on the keyboard. The key is ignored while typing, so it
  // cannot swallow a letter meant for the chat box.
  useEffect(() => {
    if (!live || mode !== "ptt") return;
    const typing = (el: EventTarget | null) =>
      el instanceof HTMLElement &&
      (el.tagName === "INPUT" ||
        el.tagName === "TEXTAREA" ||
        el.isContentEditable);
    const down = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== PTT_KEY || e.repeat || typing(e.target))
        return;
      held.current = true;
      setTalking(true);
    };
    const up = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() !== PTT_KEY || !held.current) return;
      held.current = false;
      setTalking(false);
    };
    // Releasing the key outside the window would otherwise leave the
    // microphone open.
    const blur = () => {
      if (!held.current) return;
      held.current = false;
      setTalking(false);
    };
    window.addEventListener("keydown", down);
    window.addEventListener("keyup", up);
    window.addEventListener("blur", blur);
    return () => {
      window.removeEventListener("keydown", down);
      window.removeEventListener("keyup", up);
      window.removeEventListener("blur", blur);
      blur();
    };
  }, [live, mode, setTalking]);

  if (status === "unavailable") return null;

  const others = participants.filter((p) => p.user_id !== selfID);
  const closed = !policy.open;

  if (!live) {
    return (
      <section className="voice-bar" aria-label="Voice chat">
        <button
          type="button"
          className="voice-join btn-ghost small"
          onClick={join}
          disabled={status === "asking" || status === "joining"}
        >
          {status === "asking"
            ? "Waiting for the microphone…"
            : status === "joining"
              ? "Joining voice…"
              : "Join voice"}
        </button>
        <p className="voice-note">
          {error ||
            "Your microphone stays off until you join, and nothing is recorded."}
        </p>
      </section>
    );
  }

  return (
    <section className="voice-bar live" aria-label="Voice chat">
      <div className="voice-controls">
        <button
          type="button"
          className={`voice-btn ${muted ? "off" : "on"} ${speaking && !muted && !closed ? "talking" : ""}`}
          aria-pressed={muted}
          onClick={toggleMute}
        >
          <span aria-hidden="true">{muted ? "🔇" : "🎙"}</span>
          {muted ? "Unmute" : "Mute"}
        </button>
        <button
          type="button"
          className={`voice-btn ${deafened ? "off" : ""}`}
          aria-pressed={deafened}
          onClick={toggleDeafen}
        >
          <span aria-hidden="true">{deafened ? "🔕" : "🔊"}</span>
          {deafened ? "Undeafen" : "Deafen"}
        </button>
        <button
          type="button"
          className="voice-btn"
          aria-pressed={mode === "ptt"}
          onClick={() => setMode(mode === "ptt" ? "open" : "ptt")}
        >
          {mode === "ptt" ? "Push to talk" : "Open mic"}
        </button>
        {mode === "ptt" && (
          <button
            type="button"
            className={`voice-talk ${talking ? "talking" : ""}`}
            // Hold with a finger, or hold V on a keyboard.
            onPointerDown={() => setTalking(true)}
            onPointerUp={() => setTalking(false)}
            onPointerCancel={() => setTalking(false)}
            onPointerLeave={() => setTalking(false)}
            aria-label={`Hold to talk, or hold the ${PTT_KEY.toUpperCase()} key`}
          >
            {talking ? "Talking…" : "Hold to talk"}
          </button>
        )}
        <button type="button" className="voice-btn leave" onClick={leave}>
          Leave voice
        </button>
      </div>

      {closed && (
        <p className="voice-note closed" role="status">
          {policy.reason ?? "Voice is closed for this part of the round."}
        </p>
      )}
      {blockedAudio && (
        <button type="button" className="voice-unblock" onClick={enableAudio}>
          Your browser is holding the audio back. Tap to hear the table.
        </button>
      )}
      {error && (
        <p className="voice-note error" role="alert">
          {error}
        </p>
      )}
      {!relayAvailable && (
        <p className="voice-note">
          No relay is configured, so a strict network may block voice. The game
          is unaffected.
        </p>
      )}

      <ul className="voice-people">
        {others.length === 0 && (
          <li className="voice-alone">
            Nobody else has joined voice yet. They each need to join from their
            own table.
          </li>
        )}
        {others.map((p) => (
          <li key={p.user_id} className="voice-person">
            <span
              className={`voice-dot ${p.speaking && !p.muted ? "speaking" : ""}`}
              aria-hidden="true"
            />
            <span className="voice-name">{name(p.user_id)}</span>
            <span className="voice-flags">
              {p.muted ? "muted" : p.speaking ? "speaking" : ""}
              {p.deafened ? " · deafened" : ""}
            </span>
            <label className="voice-volume">
              <span className="sr-only">Volume for {name(p.user_id)}</span>
              <input
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={volumes[p.user_id] ?? 1}
                onChange={(e) => setVolume(p.user_id, Number(e.target.value))}
              />
            </label>
          </li>
        ))}
      </ul>
    </section>
  );
}
