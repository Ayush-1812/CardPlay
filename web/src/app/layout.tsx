import type { Metadata } from "next";
import { DM_Sans, Outfit, Space_Mono } from "next/font/google";
import "./globals.css";
import "./table.css";
import "./screens.css";
import "./trump.css";
import { ErrorReporter } from "../components/error-reporter";

// Table typography (SIL Open Font License 1.1; self-hosted by next/font).
const display = Outfit({
  subsets: ["latin"],
  weight: ["500", "600", "700", "800"],
  variable: "--font-display",
});
const ui = DM_Sans({
  subsets: ["latin"],
  weight: ["400", "500", "600", "700", "800"],
  variable: "--font-ui",
});
const mono = Space_Mono({
  subsets: ["latin"],
  weight: ["400", "700"],
  variable: "--font-mono",
});

export const metadata: Metadata = {
  title: "CardPlay — a place at the table",
  description:
    "Make a room, invite your friends, and find your next card night.",
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html
      lang="en"
      className={`${display.variable} ${ui.variable} ${mono.variable}`}
    >
      <body>
        <ErrorReporter />
        {children}
      </body>
    </html>
  );
}
