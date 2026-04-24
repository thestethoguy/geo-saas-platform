"use client";
// ActivityFeed.tsx — Recent API call log

const ACTIVITY = [
  { id: "a1", method: "GET", path: "/api/v1/search?q=Hinjewadi",          status: 200, ms: 34,  ago: "2m ago" },
  { id: "a2", method: "GET", path: "/api/v1/states",                      status: 200, ms: 8,   ago: "5m ago" },
  { id: "a3", method: "GET", path: "/api/v1/states/27/districts",          status: 200, ms: 14,  ago: "12m ago" },
  { id: "a4", method: "GET", path: "/api/v1/districts/501/sub-districts",  status: 200, ms: 22,  ago: "31m ago" },
  { id: "a5", method: "GET", path: "/api/v1/search?q=Koramangal",          status: 200, ms: 29,  ago: "1h ago" },
  { id: "a6", method: "GET", path: "/api/v1/sub-districts/9902/villages",  status: 429, ms: 0,   ago: "1h ago" },
  { id: "a7", method: "GET", path: "/api/v1/states/09/districts",          status: 200, ms: 11,  ago: "2h ago" },
  { id: "a8", method: "GET", path: "/api/v1/search?q=Yelahanka",           status: 200, ms: 41,  ago: "3h ago" },
];

const STATUS_CONFIG: Record<number, { label: string; color: string; bg: string }> = {
  200: { label: "200", color: "#4ade80", bg: "rgba(34,197,94,0.12)" },
  404: { label: "404", color: "#fb923c", bg: "rgba(251,146,60,0.12)" },
  429: { label: "429", color: "#f87171", bg: "rgba(248,113,113,0.12)" },
  500: { label: "500", color: "#f43f5e", bg: "rgba(244,63,94,0.12)" },
};

export default function ActivityFeed() {
  return (
    <section
      id="activity-feed"
      className="card-glow rounded-2xl flex flex-col fade-up"
      style={{ background: "var(--bg-card)", animationDelay: "200ms" }}
    >
      {/* Header */}
      <div
        className="flex items-center justify-between px-6 py-4"
        style={{ borderBottom: "1px solid var(--border)" }}
      >
        <div>
          <h2 className="text-base font-semibold" style={{ color: "var(--text-primary)" }}>
            Recent Activity
          </h2>
          <p className="text-xs mt-0.5" style={{ color: "var(--text-muted)" }}>
            Last 24 hours · live updates
          </p>
        </div>
        <span
          className="flex items-center gap-1.5 text-xs font-medium"
          style={{ color: "var(--teal)" }}
        >
          <span className="w-1.5 h-1.5 rounded-full bg-teal-400 pulse-dot" />
          Live
        </span>
      </div>

      {/* Table header */}
      <div
        className="grid gap-4 px-6 py-2 text-xs font-medium uppercase tracking-wider"
        style={{
          color: "var(--text-muted)",
          gridTemplateColumns: "3.5rem 1fr 4rem 5rem 4rem",
          borderBottom: "1px solid var(--border)",
        }}
      >
        <span>Method</span>
        <span>Endpoint</span>
        <span className="text-right">Status</span>
        <span className="text-right">Latency</span>
        <span className="text-right">When</span>
      </div>

      {/* Rows */}
      <div className="flex flex-col divide-y" style={{ borderColor: "var(--border)" }}>
        {ACTIVITY.map((row) => {
          const sc = STATUS_CONFIG[row.status] ?? STATUS_CONFIG[500];
          return (
            <div
              key={row.id}
              className="grid items-center gap-4 px-6 py-3 text-sm transition-colors"
              style={{
                gridTemplateColumns: "3.5rem 1fr 4rem 5rem 4rem",
                color: "var(--text-secondary)",
              }}
              onMouseEnter={(e) =>
                (e.currentTarget.style.background = "var(--bg-elevated)")
              }
              onMouseLeave={(e) =>
                (e.currentTarget.style.background = "transparent")
              }
            >
              {/* Method badge */}
              <span
                className="inline-block text-xs font-bold px-2 py-0.5 rounded-md text-center"
                style={{
                  background: "rgba(99,102,241,0.12)",
                  color: "var(--accent-light)",
                }}
              >
                {row.method}
              </span>

              {/* Path */}
              <span
                className="font-mono text-xs truncate"
                style={{ color: "var(--text-secondary)" }}
                title={row.path}
              >
                {row.path}
              </span>

              {/* Status */}
              <span
                className="text-xs font-semibold px-2 py-0.5 rounded-full text-center justify-self-end"
                style={{ background: sc.bg, color: sc.color }}
              >
                {sc.label}
              </span>

              {/* Latency */}
              <span className="text-xs text-right tabular-nums" style={{ color: "var(--text-muted)" }}>
                {row.status === 429 ? "—" : `${row.ms} ms`}
              </span>

              {/* Time ago */}
              <span className="text-xs text-right" style={{ color: "var(--text-muted)" }}>
                {row.ago}
              </span>
            </div>
          );
        })}
      </div>

      {/* Footer */}
      <div
        className="px-6 py-3 text-xs text-center"
        style={{ color: "var(--text-muted)", borderTop: "1px solid var(--border)" }}
      >
        Showing last 8 requests ·{" "}
        <button id="view-all-activity" className="underline hover:opacity-80 transition-opacity" style={{ color: "var(--accent-light)" }}>
          View all logs
        </button>
      </div>
    </section>
  );
}
