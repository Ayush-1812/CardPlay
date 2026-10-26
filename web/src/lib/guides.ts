// How to play, in plain words.
//
// These are written for someone who has never seen the game before: short
// sentences, no jargon, and only what you need to take your first turn.
// Everything here matches what the server actually does -- if a rule
// changes, this changes with it.

export type GuideSection = {
  heading: string;
  // Each point is one idea, one sentence or two at most.
  points: string[];
};

export type ScreenItem = {
  label: string;
  what: string;
};

export type Guide = {
  id: string;
  name: string;
  players: string;
  // Roughly how long a game runs, so nobody starts one by accident.
  length: string;
  // One sentence: what you are trying to do.
  goal: string;
  // The shortest thing that still lets someone play.
  quickStart: string[];
  sections: GuideSection[];
  // What the things on screen mean.
  screen: ScreenItem[];
  tips: string[];
};

const monopolyDeal: Guide = {
  id: "monopoly-deal",
  name: "Monopoly Deal",
  players: "2 to 5 players",
  length: "about 15 minutes",
  goal: "Be the first to put down three complete property sets, each one a different colour.",
  quickStart: [
    "You get 5 cards to start.",
    "On your turn you draw 2 cards, then play up to 3 of them.",
    "A card can go three ways: into your bank as money, onto your table as property, or played as an action.",
    "Collect three full sets of different colours and you win.",
  ],
  sections: [
    {
      heading: "Your turn, step by step",
      points: [
        "Draw 2 cards. If your hand was empty when your turn began, you draw 5 instead.",
        "Play up to 3 cards. You can play fewer, or none at all.",
        "Every card you put down costs one of those 3 plays — money, property or action, they all count the same.",
        "You can tidy your own properties as much as you like; moving a wild card around your table is free.",
        "End your turn. If you are holding more than 7 cards, put the extra ones back at the bottom of the pile.",
      ],
    },
    {
      heading: "The three things a card can be",
      points: [
        "Money: tap it and it goes into your bank. Money is what you pay other players with.",
        "Property: tap it and it goes onto your table in its colour. Enough of one colour makes a set.",
        "Action: it does something once and is then finished — rent, a steal, a swap, and so on.",
      ],
    },
    {
      heading: "Taking money from other players",
      points: [
        "Rent: charge everyone (or one player) for a colour you own. The more of that colour you have, the more they pay.",
        "Debt Collector: one player pays you 5M.",
        "It's My Birthday: everyone pays you 2M.",
        "Sly Deal steals one property. Forced Deal swaps one of yours for one of theirs. Deal Breaker takes a whole finished set.",
        "Just Say No cancels an action aimed at you. Someone can Just Say No to your Just Say No.",
      ],
    },
    {
      heading: "Paying",
      points: [
        "You pay from your bank and from the properties on your table. You never pay from your hand.",
        "You choose which cards to hand over, not the player charging you.",
        "There is no change. If you only have a 5M for a 2M debt, you hand over the 5M.",
        "If everything you own is worth less than the debt, you hand it all over and the rest of the debt disappears.",
      ],
    },
    {
      heading: "Winning",
      points: [
        "Three complete sets, each a different colour, on your table while it is your turn.",
        "Different colours need different numbers of cards: two for brown, three for red, four for railroads, and so on. The table tells you.",
        "Until your next turn, someone can still break up your sets — so a win is only a win once it is counted.",
      ],
    },
  ],
  screen: [
    {
      label: "Your hand",
      what: "The row of cards at the bottom. Tap one to play it.",
    },
    {
      label: "Your table",
      what: "Your bank on the left, your property sets beside it.",
    },
    {
      label: "The other player's table",
      what: "Above yours. Tap a name to look at a different opponent.",
    },
    {
      label: "3 plays left",
      what: "How many cards you may still put down this turn.",
    },
    {
      label: "The clock",
      what: "How long you have before the table plays a safe move for you.",
    },
  ],
  tips: [
    "Hold on to a Just Say No. It is the only thing that stops a Deal Breaker.",
    "Money in the bank is not just money — it is what saves your properties when rent lands.",
    "A wild card counts as the colour you place it on, and you can move it later on your own turn.",
    "Watch what colours other players are close to finishing before you hand them rent.",
  ],
};

const trump: Guide = {
  id: "trump",
  name: "Trump",
  players: "exactly 4 players, in two teams",
  length: "about 10 minutes a round",
  goal: "Win 7 of the 13 tricks with your partner.",
  quickStart: [
    "You and the player sitting opposite you are a team.",
    "One team names a trump suit — that suit beats everything else.",
    "Everyone plays one card per trick. The highest card wins the trick.",
    "The first team to 7 tricks wins the round.",
  ],
  sections: [
    {
      heading: "Teams and seats",
      points: [
        "Seats 1 and 3 are one team. Seats 2 and 4 are the other.",
        "Your partner sits opposite you, so the player on your left and the one on your right are both opponents.",
        "A trick your partner wins counts for you too.",
      ],
    },
    {
      heading: "Choosing the trump suit",
      points: [
        "Everyone gets 5 cards to begin with. A toss decides which team chooses.",
        "Either player on that team may look at their 5 cards and name a suit.",
        "If you would rather your partner decided, you can pass the choice to them once. They must then choose.",
        "Chat and voice are closed while this happens, so nobody can hint at what they are holding.",
        "After the suit is named, everyone gets 8 more cards. You now hold 13.",
      ],
    },
    {
      heading: "Playing a trick",
      points: [
        "The player who chose the trump leads the first card.",
        "Going clockwise, everyone plays one card.",
        "You must play the same suit as the first card if you have one. If you have none, you may play anything, including a trump.",
        "You are never forced to play a trump.",
      ],
    },
    {
      heading: "Who wins the trick",
      points: [
        "If anyone played a trump, the highest trump wins.",
        "If nobody played a trump, the highest card of the suit that was led wins.",
        "Other suits cannot win: a King of another suit beats nothing.",
        "The winner of a trick leads the next one.",
      ],
    },
    {
      heading: "Winning the round, and the next one",
      points: [
        "The first team to 7 tricks wins the round straight away — the rest of the cards are not played.",
        "The team that won the round chooses the trump suit next round.",
        "Everyone readies up and the next round is dealt.",
      ],
    },
  ],
  screen: [
    {
      label: "The call at the top",
      what: "The trump suit for this round.",
    },
    {
      label: "The middle",
      what: "The cards played in the trick so far, and who led.",
    },
    {
      label: "Your hand",
      what: "Sorted for you: trumps first, then the matching colour, then the rest. Playable cards are lit; the others tell you why not.",
    },
    {
      label: "Team counters",
      what: "Tricks won this round. Tap your own to look back through the tricks your team has taken.",
    },
  ],
  tips: [
    "Count the trumps as they are played. When they are gone, your long suit wins tricks.",
    "Leading a suit your partner is out of lets them trump it for the team.",
    "You do not have to win every trick — you need 7, and your partner's count is yours.",
    "Think before passing the choice to your partner: they have not seen your cards.",
  ],
};

const bluff: Guide = {
  id: "bluff",
  name: "Bluff",
  players: "3 to 6 players",
  length: "about 10 minutes",
  goal: "Be the first player with no cards left — and get away with your last play.",
  quickStart: [
    "All the cards are dealt out, so everyone starts with a big hand.",
    "The table calls a rank: Aces first, then Twos, then Threes, and so on.",
    "On your turn you put 1 to 4 cards face down. They are announced as that rank — whether or not they are.",
    "Anyone can call your bluff. Whoever is wrong picks up the whole pile.",
  ],
  sections: [
    {
      heading: "The call",
      points: [
        "The rank goes Ace, 2, 3, 4, 5, 6, 7, 8, 9, 10, Jack, Queen, King, then back to Ace.",
        "It moves on after every play, whether or not anybody challenged.",
        "You cannot pass. On your turn you must put something down.",
      ],
    },
    {
      heading: "Your turn",
      points: [
        "Tap between one and four of your cards. Any cards — they do not have to match the call.",
        "Press play. The table is told how many cards you put down and what rank they are supposed to be.",
        "Your cards go face down on the pile. Nobody sees them unless someone challenges.",
        "With more than one deck in play you may put down more at once; the button tells you the limit.",
      ],
    },
    {
      heading: "Calling a bluff",
      points: [
        "After every play, everyone else gets five seconds to Accept or to Call bluff.",
        "Only the cards that were just played are turned over — never the rest of the pile.",
        "If every card matches the call, the player was honest and the challenger picks up the whole pile.",
        "If even one card is wrong, the player who put them down picks up the whole pile.",
        "Accept is final: once you accept, you cannot change your mind about that play.",
        "Say nothing and the five seconds pass as accepting.",
      ],
    },
    {
      heading: "Whose turn is next",
      points: [
        "Play always carries on with the player after the one who just played.",
        "Winning a challenge does not give you an extra turn.",
        "The call moves on to the next rank either way.",
      ],
    },
    {
      heading: "Winning",
      points: [
        "Playing your last card does not win on the spot.",
        "If nobody challenges in those five seconds, you win.",
        "If someone challenges and your cards were honest, you still win — and they pick up the pile.",
        "If someone challenges and you were lying, you pick up the pile and the game carries on.",
        "A bluff nobody calls wins the game. That is allowed.",
      ],
    },
  ],
  screen: [
    {
      label: "Call",
      what: "The rank your play has to be announced as.",
    },
    {
      label: "The pile",
      what: "Face-down cards in the middle, with how many there are.",
    },
    {
      label: "The countdown",
      what: "How long is left to accept or call the last play.",
    },
    {
      label: "Accept / Call bluff",
      what: "Your answer to the play that was just made. You only get one.",
    },
    {
      label: "The other players",
      what: "How many cards each of them is holding.",
    },
  ],
  tips: [
    "Nobody can see your cards, so a calm face costs nothing. Talking is part of the game.",
    "The pile grows fast. A challenge late in a round can hand someone twenty cards.",
    "Watch the card counts: a player down to one or two cards is about to win.",
    "If you hold three of the called rank, someone else is probably about to lie about it.",
  ],
};

export const GUIDES: Guide[] = [monopolyDeal, trump, bluff];

export function guideFor(gameID: string | undefined): Guide | undefined {
  return GUIDES.find((g) => g.id === gameID);
}
