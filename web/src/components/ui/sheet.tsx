"use client";

// Sheet: a bottom sheet on phones and a centered modal on wider screens,
// in the reference game's style. Focus moves inside on open and returns on
// close. A sheet without onClose is a required decision: no close button,
// no Escape, no backdrop dismissal.

import { ReactNode, useEffect, useRef } from "react";

export function Sheet({
  label,
  title,
  onClose,
  children,
  footer,
  tone = "default",
  wide = false,
}: {
  label: string;
  title?: ReactNode;
  onClose?: () => void;
  children: ReactNode;
  footer?: ReactNode;
  tone?: "default" | "alert";
  wide?: boolean;
}) {
  const box = useRef<HTMLDivElement>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    const previous = document.activeElement as HTMLElement | null;
    // Focus an explicitly marked control, otherwise the sheet itself, so a
    // stray Enter cannot trigger a decision such as "Accept".
    (
      box.current?.querySelector<HTMLElement>("[data-autofocus]") ?? box.current
    )?.focus({ preventScroll: true });
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close.current?.();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previous?.focus?.({ preventScroll: true });
    };
  }, []);
  return (
    <div
      className="sheet-backdrop"
      onMouseDown={(e) => {
        if (onClose && e.target === e.currentTarget) onClose();
      }}
    >
      <div
        className={`sheet tone-${tone} ${wide ? "wide" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
        ref={box}
      >
        <div className="sheet-grip" aria-hidden="true" />
        {(title || onClose) && (
          <header className="sheet-head">
            {title && <h2 className="sheet-title">{title}</h2>}
            {onClose && (
              <button
                type="button"
                className="sheet-close"
                aria-label="Close"
                onClick={onClose}
              >
                ×
              </button>
            )}
          </header>
        )}
        <div className="sheet-body">{children}</div>
        {footer && <footer className="sheet-foot">{footer}</footer>}
      </div>
    </div>
  );
}
