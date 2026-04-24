// StatCards.tsx — top-row KPI cards

interface Stat {
  id: string;
  label: string;
  value: string;
  sub: string;
  delta?: string;
  deltaPositive?: boolean;
  icon: React.ReactNode;
  accent: string;
  accentBg: string;
}

const STATS: Stat[] = [
  {
    id: "stat-total-requests",
    label: "Total Requests",
    value: "1.24M",
    sub: "All time",
    delta: "+18.3%",
    deltaPositive: true,
    accent: "#6366f1",
    accentBg: "rgba(99,102,241,0.1)",
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
        <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
      </svg>
    ),
  },
  {
    id: "stat-avg-latency",
    label: "Avg Latency",
    value: "38 ms",
    sub: "Last 24 hours",
    delta: "-4 ms",
    deltaPositive: true,
    accent: "#14b8a6",
    accentBg: "rgba(20,184,166,0.1)",
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
        <circle cx="12" cy="12" r="10" />
        <polyline points="12 6 12 12 16 14" />
      </svg>
    ),
  },
  {
    id: "stat-locations",
    label: "Locations Indexed",
    value: "650K+",
    sub: "Villages, towns & cities",
    accent: "#a78bfa",
    accentBg: "rgba(167,139,250,0.1)",
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
        <path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z" />
        <circle cx="12" cy="10" r="3" />
      </svg>
    ),
  },
  {
    id: "stat-uptime",
    label: "Uptime",
    value: "99.98%",
    sub: "Last 90 days",
    delta: "SLA met",
    deltaPositive: true,
    accent: "#22c55e",
    accentBg: "rgba(34,197,94,0.1)",
    icon: (
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.75">
        <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
        <polyline points="22 4 12 14.01 9 11.01" />
      </svg>
    ),
  },
];

export default function StatCards() {
  return (
    <div className="grid grid-cols-2 xl:grid-cols-4 gap-4">
      {STATS.map((stat, i) => (
        <div
          key={stat.id}
          id={stat.id}
          className="card-glow fade-up rounded-2xl p-5 flex flex-col gap-4"
          style={{
            background: "var(--bg-card)",
            animationDelay: `${i * 70}ms`,
          }}
        >
          {/* Icon */}
          <div
            className="w-10 h-10 rounded-xl flex items-center justify-center flex-shrink-0"
            style={{ background: stat.accentBg, color: stat.accent }}
          >
            {stat.icon}
          </div>

          {/* Value */}
          <div className="flex flex-col gap-0.5">
            <span
              className="text-2xl font-bold tabular-nums leading-none"
              style={{ color: "var(--text-primary)" }}
            >
              {stat.value}
            </span>
            <span className="text-xs" style={{ color: "var(--text-muted)" }}>
              {stat.label}
            </span>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between mt-auto pt-2" style={{ borderTop: "1px solid var(--border)" }}>
            <span className="text-xs" style={{ color: "var(--text-muted)" }}>
              {stat.sub}
            </span>
            {stat.delta && (
              <span
                className="text-xs font-medium px-2 py-0.5 rounded-full"
                style={{
                  background: stat.deltaPositive
                    ? "rgba(34,197,94,0.12)"
                    : "rgba(244,63,94,0.12)",
                  color: stat.deltaPositive ? "#4ade80" : "#fb7185",
                }}
              >
                {stat.delta}
              </span>
            )}
          </div>
        </div>
      ))}
    </div>
  );
}
