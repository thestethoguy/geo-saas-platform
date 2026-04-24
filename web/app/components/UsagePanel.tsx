// UsagePanel.tsx — Usage progress bar + quota details
// Server Component (no "use client" needed — pure display)

interface UsageData {
  used: number;
  limit: number;
  resetAt: string;
  plan: string;
  tier: "free" | "premium" | "enterprise";
}

const MOCK_USAGE: UsageData = {
  used: 4512,
  limit: 10000,
  resetAt: "Midnight UTC",
  plan: "Premium Tier",
  tier: "premium",
};

const ENDPOINT_BREAKDOWN = [
  { name: "/states",              calls: 620,  color: "#6366f1" },
  { name: "/districts",          calls: 1840, color: "#14b8a6" },
  { name: "/sub-districts",      calls: 1200, color: "#f59e0b" },
  { name: "/villages",           calls: 620,  color: "#f43f5e" },
  { name: "/search",             calls: 232,  color: "#a78bfa" },
];

const TIER_CONFIG = {
  free:       { label: "Free",       color: "#6b7280", bg: "rgba(107,114,128,0.12)", border: "rgba(107,114,128,0.25)" },
  premium:    { label: "Premium",    color: "#f59e0b", bg: "rgba(245,158,11,0.12)",  border: "rgba(245,158,11,0.3)" },
  enterprise: { label: "Enterprise", color: "#6366f1", bg: "rgba(99,102,241,0.12)", border: "rgba(99,102,241,0.3)" },
};

export default function UsagePanel() {
  const { used, limit, resetAt, tier } = MOCK_USAGE;
  const pct = Math.min((used / limit) * 100, 100);
  const tierCfg = TIER_CONFIG[tier];

  // Colour the bar red when close to limit
  const barColor = pct >= 90 ? "#f43f5e" : pct >= 70 ? "#f59e0b" : undefined; // undefined = use shimmer class

  return (
    <section
      id="usage-panel"
      className="card-glow rounded-2xl p-6 flex flex-col gap-6"
      style={{ background: "var(--bg-card)" }}
    >
      {/* Header row */}
      <div className="flex items-start justify-between gap-4 flex-wrap">
        <div>
          <h2 className="text-base font-semibold" style={{ color: "var(--text-primary)" }}>
            API Usage
          </h2>
          <p className="text-xs mt-0.5" style={{ color: "var(--text-muted)" }}>
            Resets daily at {resetAt}
          </p>
        </div>

        {/* Plan badge */}
        <span
          id="plan-badge"
          className="flex items-center gap-1.5 text-xs font-semibold px-3 py-1.5 rounded-full"
          style={{
            background: tierCfg.bg,
            color: tierCfg.color,
            border: `1px solid ${tierCfg.border}`,
          }}
        >
          {tier === "premium" && (
            <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
              <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01z" />
            </svg>
          )}
          {tierCfg.label}
        </span>
      </div>

      {/* Big numbers */}
      <div className="flex items-end gap-2">
        <span className="text-4xl font-bold tabular-nums" style={{ color: "var(--text-primary)" }}>
          {used.toLocaleString()}
        </span>
        <span className="text-lg mb-1" style={{ color: "var(--text-muted)" }}>
          / {limit.toLocaleString()} requests today
        </span>
      </div>

      {/* Progress bar */}
      <div className="flex flex-col gap-2">
        <div
          className="h-3 w-full rounded-full overflow-hidden"
          style={{ background: "var(--bg-elevated)" }}
          role="progressbar"
          aria-valuenow={used}
          aria-valuemin={0}
          aria-valuemax={limit}
          aria-label="Daily API request usage"
        >
          <div
            className={`h-full rounded-full transition-all duration-700 ${!barColor ? "shimmer" : ""}`}
            style={{
              width: `${pct.toFixed(1)}%`,
              background: barColor ?? undefined,
              boxShadow: barColor
                ? `0 0 10px ${barColor}66`
                : "0 0 12px rgba(99,102,241,0.5)",
            }}
          />
        </div>
        <div className="flex justify-between text-xs" style={{ color: "var(--text-muted)" }}>
          <span>{pct.toFixed(1)}% used</span>
          <span>{(limit - used).toLocaleString()} remaining</span>
        </div>
      </div>

      {/* Endpoint breakdown */}
      <div className="flex flex-col gap-2.5">
        <p className="text-xs font-medium uppercase tracking-wider" style={{ color: "var(--text-muted)" }}>
          Breakdown by endpoint
        </p>
        {ENDPOINT_BREAKDOWN.map((ep) => {
          const epPct = (ep.calls / used) * 100;
          return (
            <div key={ep.name} className="flex items-center gap-3">
              <span
                className="font-mono text-xs w-40 flex-shrink-0 truncate"
                style={{ color: "var(--text-secondary)" }}
              >
                {ep.name}
              </span>
              <div
                className="flex-1 h-1.5 rounded-full overflow-hidden"
                style={{ background: "var(--bg-elevated)" }}
              >
                <div
                  className="h-full rounded-full"
                  style={{ width: `${epPct.toFixed(1)}%`, background: ep.color }}
                />
              </div>
              <span
                className="text-xs w-12 text-right tabular-nums"
                style={{ color: "var(--text-secondary)" }}
              >
                {ep.calls.toLocaleString()}
              </span>
            </div>
          );
        })}
      </div>
    </section>
  );
}
