# Phase 3 manual verification

Use two separate browser profiles at http://localhost:3000. The seeded Alice and Bob accounts use the local `SEED_PASSWORD` in ignored `.env`. Mail for newly registered accounts appears at http://localhost:8025. Do not share the seed password or mail tokens.

1. Register a new account, verify with its mailed token, sign in and out, request a password reset, and confirm that the old password and old sessions stop working. Change the password in account settings and sign in again. Deleting the account should sign it out and hide its handle from search.
2. As Alice, search Bob by exact handle, request friendship, and as Bob accept it. Repeat with another account to decline, cancel, remove, block and unblock. Search and invitation views should show handles/display names, never email addresses or password data. A blocked account should no longer appear in search or receive personal invitations.
3. Create a private Monopoly Deal room with capacity two. Create an invite link and a personal friend invitation; only the target may use the latter. Bob joins and both players toggle ready. A third player cannot join a full room. Repeat at capacities three through five; capacity six must be rejected.
4. As host, rename the waiting room, change its limit, revoke a link, remove Bob, and confirm Bob cannot rejoin with the old link. Transfer host to another member, leave a room, and close a room. Nonhosts must be unable to edit, remove members or close it; former members must lose room and chat access.
5. On a phone-size viewport (360 CSS pixels or wider), check login, friends, invite joining, room controls and error messages. Disconnect and reconnect the network in a room; the socket status should recover and the room view should refresh without duplicate chat messages.

6. With two or three players ready, the host starts a match. Each browser shows its own hand, and other players' hand counts only. Take turns (bank a card, play a property, end the turn), then refresh a browser: it returns to the same seat and hand. Open the room in a second tab: the first tab says the match is open elsewhere, and **Play here instead** takes it back. Close one player's browser: the match pauses for everyone and resumes when they return. Leave the match: it ends for everyone with no winner, and the room returns to the lobby.
7. During a match, chat still works. Report or mute another player's message; muted players' messages disappear for you. With the chat scrolled out of view, a new-message badge appears.

The full lifecycle, including server restarts, is in [09-multiplayer-and-recovery.md](09-multiplayer-and-recovery.md). An automated three-browser run is in `scripts/e2e`.
