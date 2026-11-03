"use client";

import { useSyncExternalStore } from "react";
import { Moon, Sun } from "@phosphor-icons/react";

export type Theme = "dark" | "light";

const KEY = "waypoint-theme";

function subscribe(onChange: () => void) {
  const observer = new MutationObserver(onChange);
  observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
  return () => observer.disconnect();
}

const current = (): Theme => (document.documentElement.dataset.theme === "light" ? "light" : "dark");

/** Dark is the default; the choice is remembered per browser and applied before first paint by themeScript. */
export function ThemeToggle({ className = "" }: { className?: string }) {
  const theme = useSyncExternalStore(subscribe, current, () => "dark" as Theme);
  const next: Theme = theme === "dark" ? "light" : "dark";
  return (
    <button
      type="button"
      onClick={() => {
        document.documentElement.dataset.theme = next;
        try {
          localStorage.setItem(KEY, next);
        } catch {
          // Private windows can refuse storage; the theme still switches for this visit.
        }
      }}
      aria-label={`Switch to ${next} mode`}
      title={`Switch to ${next} mode`}
      className={`inline-flex size-11 items-center justify-center rounded-full text-muted transition-colors hover:text-ink ${className}`}
    >
      {theme === "dark" ? <Sun size={19} weight="regular" aria-hidden /> : <Moon size={19} weight="regular" aria-hidden />}
    </button>
  );
}

/** Runs inline in <head> so there is no flash of the wrong theme. */
export const themeScript = `try{var t=localStorage.getItem("${KEY}");document.documentElement.dataset.theme=t==="light"?"light":"dark"}catch(e){document.documentElement.dataset.theme="dark"}`;
