"use client";

// Entry: play as a guest with just a name, or sign in to an account. An
// invite link waits here and is used as soon as the player is in.

import { FormEvent, useState } from "react";
import { api, User } from "../../lib/api";

type Run = (work: () => Promise<void>) => Promise<void>;
type Mode = "login" | "register" | "verify" | "forgot" | "reset";

export function EntryScreen({
  busy,
  invited,
  run,
  onUser,
  setNotice,
}: {
  busy: boolean;
  invited: boolean;
  run: Run;
  onUser: (u: User) => void;
  setNotice: (m: string) => void;
}) {
  const [mode, setMode] = useState<Mode | null>(null);
  const submit =
    (work: (data: FormData) => Promise<void>) =>
    (e: FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      const data = new FormData(e.currentTarget);
      void run(() => work(data));
    };
  return (
    <main className="screen entry">
      <div className="entry-brand">
        <span className="brand-mark big" aria-hidden="true">
          ♣
        </span>
        <h1>CardPlay</h1>
        <p>Card games with your people. Monopoly Deal is on the table.</p>
      </div>

      {invited && (
        <p className="invite-banner" role="status">
          You&apos;re invited to a table. Enter a name to join.
        </p>
      )}

      <section className="entry-card" aria-label="Play as guest">
        <form
          onSubmit={submit(async (data) => {
            const u = await api<User>("/auth/guest", "POST", {
              display_name: data.get("display_name"),
            });
            onUser(u);
          })}
        >
          <label className="field">
            <span>Your name</span>
            <input
              name="display_name"
              required
              maxLength={24}
              autoComplete="nickname"
              placeholder="What should we call you?"
            />
          </label>
          <button className="btn-gold big" disabled={busy}>
            {invited ? "Join as guest" : "Play as guest"}
          </button>
          <p className="fine">
            No sign-up. Your guest seat lasts in this browser and is cleared
            after 7 days unused.
          </p>
        </form>
        <div className="divider">
          <span>or</span>
        </div>
        {mode === null ? (
          <div className="entry-actions">
            <button
              type="button"
              className="btn-ghost big"
              onClick={() => setMode("login")}
            >
              Sign in
            </button>
            <button
              type="button"
              className="text-button"
              onClick={() => setMode("register")}
            >
              Create an account (adds friends)
            </button>
          </div>
        ) : (
          <div className="auth-box">
            <div className="tabs" role="tablist">
              {(["login", "register", "verify", "forgot"] as const).map((m) => (
                <button
                  key={m}
                  type="button"
                  role="tab"
                  aria-selected={mode === m}
                  className={mode === m ? "chosen" : ""}
                  onClick={() => setMode(m)}
                >
                  {m === "login"
                    ? "Sign in"
                    : m === "register"
                      ? "Create account"
                      : m === "verify"
                        ? "Verify email"
                        : "Forgot password"}
                </button>
              ))}
            </div>
            {mode === "login" && (
              <form
                onSubmit={submit(async (data) => {
                  await api("/auth/login", "POST", {
                    email: data.get("email"),
                    password: data.get("password"),
                  });
                  onUser(await api<User>("/me"));
                })}
              >
                <label className="field">
                  <span>Email</span>
                  <input
                    name="email"
                    type="email"
                    required
                    autoComplete="email"
                  />
                </label>
                <label className="field">
                  <span>Password</span>
                  <input
                    name="password"
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="current-password"
                  />
                </label>
                <button className="btn-gold big" disabled={busy}>
                  Take your seat →
                </button>
              </form>
            )}
            {mode === "register" && (
              <form
                onSubmit={submit(async (data) => {
                  await api("/auth/register", "POST", {
                    email: data.get("email"),
                    password: data.get("password"),
                    handle: data.get("handle"),
                    display_name: data.get("display_name"),
                  });
                  setNotice("Check your email for a verification token.");
                  setMode("verify");
                })}
              >
                <div className="form-row">
                  <label className="field">
                    <span>Display name</span>
                    <input
                      name="display_name"
                      required
                      maxLength={60}
                      autoComplete="nickname"
                    />
                  </label>
                  <label className="field">
                    <span>Handle</span>
                    <input
                      name="handle"
                      required
                      pattern="[a-z0-9_]{3,24}"
                      autoComplete="username"
                      title="3-24 lowercase letters, numbers or _"
                    />
                  </label>
                </div>
                <label className="field">
                  <span>Email</span>
                  <input
                    name="email"
                    type="email"
                    required
                    autoComplete="email"
                  />
                </label>
                <label className="field">
                  <span>Password</span>
                  <input
                    name="password"
                    type="password"
                    required
                    minLength={12}
                    maxLength={128}
                    autoComplete="new-password"
                  />
                </label>
                <button className="btn-gold big" disabled={busy}>
                  Create account →
                </button>
              </form>
            )}
            {(mode === "verify" || mode === "reset") && (
              <form
                onSubmit={submit(async (data) => {
                  if (mode === "verify") {
                    await api("/auth/verify", "POST", {
                      token: data.get("token"),
                    });
                    setNotice("Email verified. You can sign in now.");
                  } else {
                    await api("/auth/password/reset", "POST", {
                      token: data.get("token"),
                      password: data.get("password"),
                    });
                    setNotice("Password updated. Sign in again.");
                  }
                  setMode("login");
                })}
              >
                <label className="field">
                  <span>Email token</span>
                  <input name="token" required autoComplete="one-time-code" />
                </label>
                {mode === "reset" && (
                  <label className="field">
                    <span>New password</span>
                    <input
                      name="password"
                      type="password"
                      required
                      minLength={12}
                      maxLength={128}
                      autoComplete="new-password"
                    />
                  </label>
                )}
                <button className="btn-gold big" disabled={busy}>
                  {mode === "verify" ? "Verify email →" : "Save new password →"}
                </button>
              </form>
            )}
            {mode === "forgot" && (
              <form
                onSubmit={submit(async (data) => {
                  await api("/auth/password/forgot", "POST", {
                    email: data.get("email"),
                  });
                  setNotice(
                    "If the account exists, a reset token is on its way.",
                  );
                  setMode("reset");
                })}
              >
                <label className="field">
                  <span>Email</span>
                  <input
                    name="email"
                    type="email"
                    required
                    autoComplete="email"
                  />
                </label>
                <button className="btn-gold big" disabled={busy}>
                  Send reset token →
                </button>
              </form>
            )}
          </div>
        )}
      </section>
    </main>
  );
}

// A registered account that has not confirmed its email yet.
export function VerifyScreen({
  busy,
  run,
  onUser,
  onSignOut,
  setNotice,
}: {
  busy: boolean;
  run: Run;
  onUser: (u: User) => void;
  onSignOut: () => void;
  setNotice: (m: string) => void;
}) {
  return (
    <main className="screen entry">
      <section className="entry-card" aria-label="Verify your email">
        <h1>One more step.</h1>
        <p>Verify your email to create rooms and invite friends.</p>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const token = new FormData(e.currentTarget).get("token");
            void run(async () => {
              await api("/auth/verify", "POST", { token });
              onUser(await api<User>("/me"));
            });
          }}
        >
          <label className="field">
            <span>Verification token</span>
            <input name="token" required autoComplete="one-time-code" />
          </label>
          <button className="btn-gold big" disabled={busy}>
            Verify email
          </button>
        </form>
        <div className="entry-actions">
          <button
            type="button"
            className="text-button"
            disabled={busy}
            onClick={() =>
              void run(async () => {
                await api("/auth/verify/resend", "POST", {
                  email: (await api<{ email: string }>("/me")).email,
                });
                setNotice(
                  "If verification is still needed, a fresh token is on its way.",
                );
              })
            }
          >
            Resend token
          </button>
          <button
            type="button"
            className="text-button"
            disabled={busy}
            onClick={onSignOut}
          >
            Sign out
          </button>
        </div>
      </section>
    </main>
  );
}
