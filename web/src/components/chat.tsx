"use client";

// Room chat: used in the waiting room and inside the table's drawer. Plain
// text only; quick replies send a short message with one tap.

import { FormEvent, Ref, useRef } from "react";
import { ChatMessage } from "../lib/api";

const QUICK = ["👍", "😂", "😮", "Nice move!", "Your turn!", "GG"];

export function ChatPanel({
  messages,
  me,
  busy,
  panelRef,
  logRef,
  onSend,
  onReport,
  onMute,
}: {
  messages: ChatMessage[];
  me: string;
  busy: boolean;
  panelRef?: Ref<HTMLElement>;
  logRef?: Ref<HTMLDivElement>;
  onSend: (body: string) => Promise<boolean>;
  onReport: (msg: ChatMessage) => void;
  onMute: (msg: ChatMessage) => void;
}) {
  const input = useRef<HTMLInputElement>(null);
  const submit = async (e: FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const body = input.current?.value.trim() ?? "";
    if (!body) return;
    if ((await onSend(body)) && input.current) input.current.value = "";
  };
  return (
    <section className="panel chat-panel" ref={panelRef}>
      <div className="section-heading">
        <h2>Table talk</h2>
        <span className="muted">Room chat</span>
      </div>
      <div
        className="chat-log"
        ref={logRef}
        role="log"
        aria-live="polite"
        aria-label="Room chat"
      >
        {messages.length === 0 ? (
          <p className="empty">Say hello. The table is yours.</p>
        ) : (
          messages.map((msg) => (
            <div
              className={`message ${msg.user_id === me ? "mine" : ""}`}
              key={msg.id}
            >
              <strong>{msg.user_id === me ? "You" : msg.display_name}</strong>
              <time>
                {new Date(msg.created_at).toLocaleTimeString([], {
                  hour: "2-digit",
                  minute: "2-digit",
                })}
              </time>
              <p>{msg.body}</p>
              {msg.user_id !== me && (
                <span className="message-tools">
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => onReport(msg)}
                  >
                    Report
                  </button>
                  <button
                    type="button"
                    className="text-button"
                    disabled={busy}
                    onClick={() => onMute(msg)}
                  >
                    Mute
                  </button>
                </span>
              )}
            </div>
          ))
        )}
      </div>
      <div className="quick-replies" role="group" aria-label="Quick replies">
        {QUICK.map((q) => (
          <button
            key={q}
            type="button"
            disabled={busy}
            onClick={() => void onSend(q)}
          >
            {q}
          </button>
        ))}
      </div>
      <form className="chat-form" onSubmit={(e) => void submit(e)}>
        <label className="sr-only" htmlFor="chat-body">
          Message
        </label>
        <input
          id="chat-body"
          name="body"
          ref={input}
          required
          maxLength={500}
          autoComplete="off"
          placeholder="Send a little table talk…"
        />
        <button className="primary" disabled={busy}>
          Send
        </button>
      </form>
    </section>
  );
}
