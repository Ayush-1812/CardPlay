import type { Metadata } from "next";
import "./globals.css";
export const metadata: Metadata = {
  title: "CardPlay — a place at the table",
  description:
    "Make a room, invite your friends, and find your next card night.",
};
export default function RootLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
