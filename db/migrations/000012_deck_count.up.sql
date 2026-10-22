-- How many decks a room's game is played with. Bluff can be played with
-- more than one 52-card deck, which suits a crowded table: more cards of
-- every rank, so a bigger claim is possible. Games with a fixed deck
-- ignore it.
ALTER TABLE rooms
 ADD COLUMN deck_count smallint NOT NULL DEFAULT 1
 CHECK (deck_count BETWEEN 1 AND 4);
