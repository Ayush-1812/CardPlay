"use client";

// How to play, as a sheet anyone can open: from the games list before
// choosing, from the room while waiting, and from the table mid-game when
// it is your turn and you are not sure what to do.

import { useState } from "react";
import { Sheet } from "./ui/sheet";
import { guideFor, type Guide } from "../lib/guides";

export function HowToPlaySheet({
  guide,
  onClose,
}: {
  guide: Guide;
  onClose: () => void;
}) {
  return (
    <Sheet
      label={`How to play ${guide.name}`}
      title={`How to play ${guide.name}`}
      onClose={onClose}
      wide
    >
      <p className="guide-meta">
        {guide.players} · {guide.length}
      </p>

      <section className="guide-goal" aria-label="What you are trying to do">
        <h3>What you are trying to do</h3>
        <p>{guide.goal}</p>
      </section>

      <section className="guide-quick" aria-label="In short">
        <h3>In short</h3>
        <ol>
          {guide.quickStart.map((line) => (
            <li key={line}>{line}</li>
          ))}
        </ol>
      </section>

      {guide.sections.map((section) => (
        <section key={section.heading} className="guide-section">
          <h3>{section.heading}</h3>
          <ul>
            {section.points.map((point) => (
              <li key={point}>{point}</li>
            ))}
          </ul>
        </section>
      ))}

      <section className="guide-section" aria-label="What you see on screen">
        <h3>What you see on screen</h3>
        <dl className="guide-screen">
          {guide.screen.map((item) => (
            <div key={item.label}>
              <dt>{item.label}</dt>
              <dd>{item.what}</dd>
            </div>
          ))}
        </dl>
      </section>

      <section className="guide-section" aria-label="Tips">
        <h3>Worth knowing</h3>
        <ul>
          {guide.tips.map((tip) => (
            <li key={tip}>{tip}</li>
          ))}
        </ul>
      </section>

      <section className="guide-section" aria-label="If something goes wrong">
        <h3>If something goes wrong</h3>
        <ul>
          <li>
            Closing the tab or losing your connection does not lose your place:
            open the room again and your cards are where you left them.
          </li>
          <li>
            The table waits for a player who has dropped out, and says so. Their
            cards are never played for them.
          </li>
          <li>
            Chat and voice are there for the whole table. Voice is off until you
            press Join voice.
          </li>
        </ul>
      </section>
    </Sheet>
  );
}

// HowToPlayButton is the button and the sheet together, so a screen only
// has to decide where to put it.
export function HowToPlayButton({
  gameID,
  className = "text-button",
  label = "How to play",
}: {
  gameID: string | undefined;
  className?: string;
  label?: string;
}) {
  const [open, setOpen] = useState(false);
  const guide = guideFor(gameID);
  if (!guide) return null;
  return (
    <>
      <button
        type="button"
        className={className}
        aria-haspopup="dialog"
        onClick={(e) => {
          // On a game card the whole card is clickable; opening the rules
          // should not also start a room.
          e.stopPropagation();
          setOpen(true);
        }}
      >
        {label}
      </button>
      {open && <HowToPlaySheet guide={guide} onClose={() => setOpen(false)} />}
    </>
  );
}
