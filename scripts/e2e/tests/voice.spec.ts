import { expect, test, type Page } from "@playwright/test";
import { latestState, newPlayer, playAsGuest, type Player } from "./helpers";

// In-game voice chat, driven through the real screens with Chromium's fake
// microphone. What cannot be checked in a browser farm is listed in
// docs/14-voice-chat.md; everything here is checked.

// The audio elements the client creates for each peer say whether media is
// actually arriving: a remote track stays muted until packets flow.
async function peerAudio(page: Page) {
  return page.evaluate(() =>
    [...document.querySelectorAll("audio[data-voice-peer]")].map((el) => {
      const audio = el as HTMLAudioElement;
      const stream = audio.srcObject as MediaStream | null;
      return {
        peer: audio.getAttribute("data-voice-peer") ?? "",
        volume: audio.volume,
        tracks: (stream?.getAudioTracks() ?? []).map((t) => ({
          live: t.readyState === "live",
          receiving: !t.muted,
        })),
      };
    }),
  );
}

// This machine's operating system refuses microphone access to browsers, so
// getUserMedia is answered with a synthesised track: a real MediaStreamTrack
// carrying a real tone, which WebRTC encodes and sends like any other. Every
// code path from getUserMedia onward is the production one; only the device
// behind it is not a physical microphone. See docs/14-voice-chat.md.
async function fakeMic(p: Player) {
  await p.context.addInitScript(() => {
    const original = navigator.mediaDevices.getUserMedia.bind(
      navigator.mediaDevices,
    );
    (navigator.mediaDevices as MediaDevices).getUserMedia = async (
      constraints?: MediaStreamConstraints,
    ) => {
      if (!constraints?.audio) return original(constraints);
      const ctx = new AudioContext();
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      const dest = ctx.createMediaStreamDestination();
      osc.frequency.value = 440;
      gain.gain.value = 0.3;
      osc.connect(gain).connect(dest);
      osc.start();
      return dest.stream;
    };
  });
}

const voice = (p: Player) => p.page.locator(".voice-bar");
const joinVoice = async (p: Player) => {
  await voice(p).getByRole("button", { name: "Join voice" }).click();
  await expect(
    voice(p).getByRole("button", { name: "Leave voice" }),
  ).toBeVisible({
    timeout: 20_000,
  });
};

test.describe.serial("voice chat at a four-seat table", () => {
  const players: Player[] = [];
  let link: string;

  test.beforeAll(async ({ browser }) => {
    for (const name of ["va", "vb", "vc", "vd"]) {
      const p = await newPlayer(browser, name, {
        viewport: { width: 1280, height: 900 },
      });
      await fakeMic(p);
      players.push(p);
    }
  });
  test.afterAll(async () => {
    for (const p of players) await p?.context.close();
  });

  test("four players join one room's voice channel", async () => {
    const host = players[0];
    await playAsGuest(host);
    await host.page.getByRole("button", { name: /Trump/ }).click();
    await host.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      host.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    link = await host.page.getByLabel("Invite link").inputValue();
    for (const p of players.slice(1)) {
      await p.page.goto(link);
      await p.page.getByLabel("Your name").fill(p.name);
      await p.page.getByRole("button", { name: "Join as guest" }).click();
      await expect(p.page.locator(".room-head .pill")).toHaveText("Connected");
    }

    // Voice is opt-in: until someone joins there is no microphone and no
    // channel, and the bar says so.
    await expect(voice(host)).toContainText("microphone stays off");
    for (const p of players) await joinVoice(p);

    // Everyone sees the other three.
    for (const p of players)
      await expect(p.page.locator(".voice-person")).toHaveCount(3, {
        timeout: 20_000,
      });
  });

  test("audio actually flows between the peers", async () => {
    // Three peer connections each, and media arriving on them. A remote
    // track reports muted until packets are received, so this is a real
    // check that the WebRTC path came up, not just that the UI drew a row.
    for (const p of players) {
      await expect
        .poll(
          async () => {
            const peers = await peerAudio(p.page);
            return peers.filter((x) =>
              x.tracks.some((t) => t.live && t.receiving),
            ).length;
          },
          { timeout: 30_000, message: `${p.name} is not receiving audio` },
        )
        .toBe(3);
    }
  });

  test("muting is seen by the others, and volume is per player", async () => {
    const [host, second] = players;
    await voice(host).getByRole("button", { name: "Mute" }).click();
    await expect(
      voice(host).getByRole("button", { name: "Unmute" }),
    ).toBeVisible();
    // The others are told, in words rather than only a colour.
    for (const p of players.slice(1))
      await expect(
        voice(p).locator(".voice-person", { hasText: "va" }).first(),
      ).toContainText("muted", { timeout: 15_000 });
    await voice(host).getByRole("button", { name: "Unmute" }).click();

    // Each player's volume is their own; it reaches the audio element.
    const row = voice(second).locator(".voice-person").first();
    await row.locator("input[type=range]").evaluate((el) => {
      const input = el as HTMLInputElement;
      const setter = Object.getOwnPropertyDescriptor(
        window.HTMLInputElement.prototype,
        "value",
      )!.set!;
      setter.call(input, "0.25");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    await expect
      .poll(async () =>
        (await peerAudio(second.page)).some(
          (x) => Math.abs(x.volume - 0.25) < 0.01,
        ),
      )
      .toBe(true);
  });

  test("push-to-talk only transmits while held", async () => {
    const host = players[0];
    await voice(host).getByRole("button", { name: "Open mic" }).click();
    const hold = voice(host).getByRole("button", { name: /Hold to talk/ });
    await expect(hold).toBeVisible();
    // Held: the microphone transmits, and the others see it. Released: it
    // stops. The indicator follows the track, so this is the gate itself.
    const listenerRow = voice(players[1])
      .locator(".voice-person", { hasText: "va" })
      .first();
    await hold.dispatchEvent("pointerdown");
    await expect(hold).toHaveText("Talking…");
    await expect(listenerRow).toContainText("speaking", { timeout: 15_000 });
    await hold.dispatchEvent("pointerup");
    await expect(hold).toHaveText("Hold to talk");
    await expect(listenerRow).not.toContainText("speaking", {
      timeout: 15_000,
    });
    await voice(host).getByRole("button", { name: "Push to talk" }).click();
    await expect(
      voice(host).getByRole("button", { name: "Open mic" }),
    ).toBeVisible();
  });

  test("Trump closes voice while the trump is chosen, and reopens it", async () => {
    for (const p of players)
      await p.page.getByRole("button", { name: "I'm ready" }).click();
    await players[0].page.getByRole("button", { name: "Start match" }).click();
    for (const p of players)
      await expect(p.page.locator(".trump-table")).toBeVisible();

    // Selection has begun: every client is told voice is closed, without
    // being asked.
    for (const p of players)
      await expect(voice(p).locator(".voice-note.closed")).toContainText(
        "while the trump is chosen",
        { timeout: 20_000 },
      );
    // And the connections are gone, so there is nothing left to carry audio.
    for (const p of players)
      await expect
        .poll(async () => (await peerAudio(p.page)).length, { timeout: 20_000 })
        .toBe(0);

    // Choose the suit; voice comes back for the rest of the round.
    const pub = (p: Player) => latestState(p).view.public;
    const self = (p: Player) => latestState(p).view.self;
    const chooser = players.find(
      (p) => self(p).seat % 2 === pub(p).entitled_team,
    )!;
    await chooser.page
      .getByRole("button", { name: /Hearts|Spades|Clubs|Diamonds/ })
      .first()
      .click();
    for (const p of players)
      await expect(p.page.locator(".trump-indicator")).toBeVisible();
    for (const p of players)
      await expect(voice(p).locator(".voice-note.closed")).toHaveCount(0, {
        timeout: 20_000,
      });
    // The mesh rebuilds itself without anyone pressing anything.
    for (const p of players)
      await expect
        .poll(async () => (await peerAudio(p.page)).length, { timeout: 30_000 })
        .toBe(3);
  });

  test("a dropped connection rejoins the channel", async () => {
    const p = players[3];
    await p.page.evaluate(() => {
      const w = window as unknown as { __sockets: WebSocket[] };
      for (const s of w.__sockets) s.close();
    });
    await expect(
      p.page.locator(".room-head .pill, .trump-bar .conn"),
    ).toContainText(/Connect/i, { timeout: 20_000 });
    // Back in the channel, and the others see them again.
    await expect(
      voice(p).getByRole("button", { name: "Leave voice" }),
    ).toBeVisible({
      timeout: 30_000,
    });
    for (const other of players.slice(0, 3))
      await expect(voice(other).locator(".voice-person")).toHaveCount(3, {
        timeout: 30_000,
      });
  });

  test("leaving voice releases the microphone and the connections", async () => {
    const p = players[2];
    await voice(p).getByRole("button", { name: "Leave voice" }).click();
    await expect(
      voice(p).getByRole("button", { name: "Join voice" }),
    ).toBeVisible();
    // No peer audio elements are left behind.
    await expect.poll(async () => (await peerAudio(p.page)).length).toBe(0);
    // The others see a channel of three.
    for (const other of [players[0], players[1], players[3]])
      await expect(voice(other).locator(".voice-person")).toHaveCount(2, {
        timeout: 20_000,
      });
  });

  test("no page errors anywhere", async () => {
    for (const p of players)
      expect(p.errors, `${p.name} page errors`).toEqual([]);
  });
});

// A player in another room must not reach this one's channel, and a player
// who refuses the microphone must still be able to play.
test.describe.serial("voice is scoped and optional", () => {
  test("a denied microphone does not stop the game", async ({ browser }) => {
    const p = await newPlayer(browser, "vdenied", {
      viewport: { width: 390, height: 844 },
    });
    // The browser refuses the microphone, as it does when a player says no
    // or has blocked the site.
    await p.context.addInitScript(() => {
      navigator.mediaDevices.getUserMedia = () =>
        Promise.reject(
          new DOMException("Permission denied", "NotAllowedError"),
        );
    });
    await playAsGuest(p);
    await p.page.getByRole("button", { name: /Trump/ }).click();
    await p.page.getByRole("button", { name: "Create room" }).click();
    await expect(
      p.page.getByRole("heading", { level: 1, name: /'s table$/ }),
    ).toBeVisible();
    await voice(p).getByRole("button", { name: "Join voice" }).click();
    // Either refusal is fine to show; what matters is that it names the
    // layer that said no and the game carries on.
    await expect(voice(p)).toContainText(
      /blocked from using your microphone|blocking the microphone|could not be opened/,
      {
        timeout: 20_000,
      },
    );
    // The room is still usable: the refusal is not an error state for the game.
    await expect(p.page.getByLabel("Invite link")).toBeVisible();
    expect(p.errors).toEqual([]);
    await p.context.close();
  });

  test("another room's players are a separate channel", async ({ browser }) => {
    const a = await newPlayer(browser, "vroom1");
    const b = await newPlayer(browser, "vroom2");
    await fakeMic(a);
    await fakeMic(b);
    for (const p of [a, b]) {
      await playAsGuest(p);
      await p.page.getByRole("button", { name: /Trump/ }).click();
      await p.page.getByRole("button", { name: "Create room" }).click();
      await expect(
        p.page.getByRole("heading", { level: 1, name: /'s table$/ }),
      ).toBeVisible();
      await joinVoice(p);
    }
    // Each is alone in their own room's channel.
    for (const p of [a, b]) {
      await expect(voice(p).locator(".voice-person")).toHaveCount(0);
      await expect(voice(p)).toContainText("Nobody else has joined voice yet");
      expect(await peerAudio(p.page)).toEqual([]);
    }
    for (const p of [a, b]) await p.context.close();
  });
});
