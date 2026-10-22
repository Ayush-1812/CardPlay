// Types and helpers for in-game voice chat.
//
// Audio travels directly between players (or through a TURN relay); the
// server only relays the negotiation. Nothing here records or uploads audio,
// and the microphone is opened only after the player asks for it.

import { api } from "./api";

// Participant is one player in the voice channel, as the server publishes
// them. Speaking comes from that player's own level detection.
export type VoiceParticipant = {
  user_id: string;
  muted: boolean;
  deafened: boolean;
  speaking: boolean;
};

// VoicePolicy is the running game's answer about speech. Trump closes it
// while the trump is chosen.
export type VoicePolicy = { open: boolean; reason?: string };

export type IceConfig = {
  enabled: boolean;
  ice_servers: RTCIceServer[];
  ttl_seconds: number;
  relay_available: boolean;
  max_participants: number;
};

export type SignalKind = "offer" | "answer" | "candidate" | "bye";

// Fetches ICE servers and a TURN credential that expires. Called when the
// player joins and again before the credential runs out.
export async function fetchIce(): Promise<IceConfig> {
  return api<IceConfig>("/voice/ice");
}

// Microphone constraints. The browser's own processing is asked for; a
// device that cannot do it still works, just less pleasantly.
export const MIC_CONSTRAINTS: MediaStreamConstraints = {
  audio: {
    echoCancellation: true,
    noiseSuppression: true,
    autoGainControl: true,
  },
  video: false,
};

// Turns a getUserMedia failure into something worth showing a player.
export async function micErrorMessage(err: unknown): Promise<string> {
  const name = (err as { name?: string })?.name ?? "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      // A refusal has two very different causes, and telling a player to
      // look in the address bar is wrong for one of them. If the site is
      // still allowed to ask, nothing was ever asked: the operating system
      // or a policy refused on the browser's behalf.
      return (await siteIsBlocked())
        ? "This site is blocked from using your microphone. Open the icon at the left of the address bar, set Microphone to Allow, then join voice again."
        : "Your system is blocking the microphone for browsers, so you were never asked. On Windows: Settings, Privacy & security, Microphone, turn on microphone access for apps. On macOS: System Settings, Privacy & Security, Microphone. Then join voice again.";
    case "NotFoundError":
    case "OverconstrainedError":
      return "No microphone was found. Plug one in or choose another device, then join voice again.";
    case "NotReadableError":
      return "Another app is using your microphone. Close it and join voice again.";
    default:
      if (!isSecureContext())
        return "Microphones only work on a secure page. Open the site over https (or on localhost) and join voice again.";
      return "The microphone could not be opened. You can keep playing; voice is optional.";
  }
}

// siteIsBlocked reports whether this origin itself is in the browser's
// blocked list, as opposed to the request being refused above the browser.
// Browsers without the Permissions API fall back to the site answer, which
// is the more common cause.
async function siteIsBlocked(): Promise<boolean> {
  try {
    const status = await navigator.permissions.query({
      name: "microphone" as PermissionName,
    });
    return status.state === "denied";
  } catch {
    return true;
  }
}

// A page served over plain http (other than localhost) cannot open a
// microphone at all, and navigator.mediaDevices is missing entirely.
function isSecureContext(): boolean {
  return (
    typeof window === "undefined" ||
    (window.isSecureContext && !!navigator.mediaDevices?.getUserMedia)
  );
}

// Who makes the offer. Both sides learn about each other at the same moment,
// so without a rule both would offer at once and glare. The lower user id
// offers; the other waits.
export function shouldOffer(self: string, peer: string): boolean {
  return self < peer;
}

// speakingFrom reports whether a frame of audio is loud enough to count as
// speech. The threshold is deliberately low: it lights an indicator, it does
// not gate transmission.
export function isSpeaking(samples: Float32Array): boolean {
  let sum = 0;
  for (const s of samples) sum += s * s;
  return Math.sqrt(sum / samples.length) > 0.02;
}
