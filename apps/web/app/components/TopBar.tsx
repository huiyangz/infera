"use client";
import Link from "next/link";
import { useTheme } from "next-themes";
import { useEffect, useState } from "react";
import { logout } from "@/lib/api";

export function TopBar() {
  const { theme, setTheme } = useTheme();
  const [mounted, setMounted] = useState(false);
  useEffect(() => setMounted(true), []);

  return (
    <header
      className="flex items-center justify-between border-b px-6 py-3"
      style={{ borderColor: "var(--border)", background: "var(--bg)" }}
    >
      <Link href="/" className="font-bold tracking-tight">
        infera
      </Link>
      <div className="flex items-center gap-3 text-sm">
        {mounted && (
          <button
            className="border rounded px-2 py-1"
            style={{ borderColor: "var(--border)" }}
            onClick={() => setTheme(theme === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? "☀" : "☾"}
          </button>
        )}
        <button
          className="text-[color:var(--muted)]"
          onClick={async () => { await logout(); location.href = "/login"; }}
        >
          登出
        </button>
      </div>
    </header>
  );
}
