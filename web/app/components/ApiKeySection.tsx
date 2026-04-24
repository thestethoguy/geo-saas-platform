"use client";

import { useState } from "react";

// ── Mock data ────────────────────────────────────────────────────────────────
// NOTE: These are fictional demo values — no real secrets here.
const MOCK_KEYS = [
  {
    id: "key-1",
    label: "Production",
    value: "geo_key_prod_a3f8b2c9d1e4f7a0b5c2d8e3f6a1b4c7",
    created: "12 Jan 2026",
    lastUsed: "2 minutes ago",
    active: true,
  },
  {
    id: "key-2",
    label: "Staging",
    value: "geo_key_stg_f9e3c7b2a1d4f8e0b5a2c9d7f3e1b8c4",
    created: "28 Feb 2026",
    lastUsed: "3 hours ago",
    active: true,
  },
  {
    id: "key-3",
    label: "Dev / Local",
    value: "geo_key_dev_00000000000000000000000000000000",
    created: "10 Apr 2026",
    lastUsed: "Yesterday",
    active: false,
  },
];

function maskKey(key: string) {
  return key.slice(0, 14) + "•".repeat(20) + key.slice(-4);
}

// ── Sub-component: single key row ────────────────────────────────────────────
function KeyRow({
  apiKey,
  delay,
}: {
  apiKey: (typeof MOCK_KEYS)[0];
  delay: number;
}) {
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);

  function handleCopy() {
    navigator.clipboard.writeText(apiKey.value);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <div
      className="fade-up card-glow rounded-xl p-5 flex flex-col gap-3"
      style={{
        background: "var(--bg-card)",
        animationDelay: `${delay}ms`,
      }}
    >
      {/* Top row */}
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="flex items-center gap-3">
          {/* Status dot */}
          <span
            className={`w-2 h-2 rounded-full flex-shrink-0 ${
              apiKey.active ? "pulse-dot bg-green-400" : "bg-gray-600"
            }`}
          />
          <span className="font-semibold text-sm" style={{ color: "var(--text-primary)" }}>
            {apiKey.label}
          </span>
          {apiKey.active && (
            <span
              className="text-xs px-2 py-0.5 rounded-full font-medium"
              style={{
                background: "rgba(34,197,94,0.12)",
                color: "#4ade80",
                border: "1px solid rgba(34,197,94,0.2)",
              }}
            >
              Active
            </span>
          )}
        </div>
        <div className="flex items-center gap-2 text-xs" style={{ color: "var(--text-muted)" }}>
          <span>Created {apiKey.created}</span>
          <span>·</span>
          <span>Last used {apiKey.lastUsed}</span>
        </div>
      </div>

      {/* Key display */}
      <div
        className="flex items-center gap-3 rounded-lg px-4 py-3 font-mono text-sm flex-wrap"
        style={{
          background: "var(--bg-elevated)",
          border: "1px solid var(--border)",
          color: "var(--text-secondary)",
          letterSpacing: "0.02em",
        }}
      >
        <span className="flex-1 truncate">
          {revealed ? apiKey.value : maskKey(apiKey.value)}
        </span>

        {/* Reveal toggle */}
        <button
          id={`reveal-${apiKey.id}`}
          onClick={() => setRevealed((r) => !r)}
          className="text-xs px-2 py-1 rounded-md transition-colors"
          style={{
            background: "var(--bg-surface)",
            color: "var(--text-secondary)",
            border: "1px solid var(--border-bright)",
          }}
          title={revealed ? "Hide key" : "Reveal key"}
        >
          {revealed ? "Hide" : "Show"}
        </button>

        {/* Copy button */}
        <button
          id={`copy-${apiKey.id}`}
          onClick={handleCopy}
          className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-md font-medium transition-all"
          style={{
            background: copied
              ? "rgba(34,197,94,0.15)"
              : "rgba(99,102,241,0.15)",
            color: copied ? "#4ade80" : "var(--accent-light)",
            border: `1px solid ${copied ? "rgba(34,197,94,0.3)" : "rgba(99,102,241,0.3)"}`,
          }}
        >
          {copied ? (
            <>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                <polyline points="20 6 9 17 4 12" />
              </svg>
              Copied!
            </>
          ) : (
            <>
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
              </svg>
              Copy
            </>
          )}
        </button>
      </div>
    </div>
  );
}

// ── Main export ───────────────────────────────────────────────────────────────
export default function ApiKeySection() {
  return (
    <section id="api-keys" className="flex flex-col gap-4">
      {/* Section header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-base font-semibold" style={{ color: "var(--text-primary)" }}>
            Active API Keys
          </h2>
          <p className="text-xs mt-0.5" style={{ color: "var(--text-muted)" }}>
            Include in requests as:{" "}
            <code
              className="px-1.5 py-0.5 rounded text-xs"
              style={{ background: "var(--bg-elevated)", color: "var(--accent-light)" }}
            >
              Authorization: Bearer sk_live_…
            </code>
          </p>
        </div>
        <button
          id="create-key-btn"
          className="flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-lg transition-all"
          style={{
            background: "var(--accent)",
            color: "#fff",
            boxShadow: "0 0 16px var(--accent-glow)",
          }}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          New Key
        </button>
      </div>

      {/* Key rows */}
      <div className="flex flex-col gap-3">
        {MOCK_KEYS.map((k, i) => (
          <KeyRow key={k.id} apiKey={k} delay={i * 60} />
        ))}
      </div>
    </section>
  );
}
