'use client';

import { useState, useTransition, useRef } from 'react';
import Sidebar from '../components/Sidebar';
import StatCards from '../components/StatCards';
import ActivityFeed from '../components/ActivityFeed';
import { searchGeoData, type ActionResponse } from '../actions';

// ── JSON syntax highlighter (safe — escapes HTML before coloring) ─────────────
function colorize(raw: string): string {
  const esc = raw.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;');
  return esc
    // Keys
    .replace(/("[\w\s]+")\s*:/g, '<span style="color:#818cf8">$1</span>:')
    // String values
    .replace(/:\s*("(?:[^"\\]|\\.)*")/g, ': <span style="color:#34d399">$1</span>')
    // Booleans / null
    .replace(/\b(true|false)\b/g, '<span style="color:#fbbf24">$1</span>')
    .replace(/\bnull\b/g, '<span style="color:#f87171">null</span>')
    // Numbers
    .replace(/:\s*(-?\d+(?:\.\d+)?)\b/g, ': <span style="color:#60a5fa">$1</span>');
}

function JsonViewer({ data }: { data: unknown }) {
  const lines = JSON.stringify(data, null, 2).split('\n');
  return (
    <pre
      className="overflow-auto text-xs leading-relaxed p-5 rounded-xl"
      style={{
        background: 'var(--bg-base)',
        border: '1px solid var(--border)',
        maxHeight: '420px',
        fontFamily: "'Cascadia Code','Fira Code','JetBrains Mono',monospace",
      }}
    >
      <code>
        {lines.map((line, i) => (
          <div key={i} className="flex">
            <span className="select-none w-7 mr-4 text-right flex-shrink-0" style={{ color: 'var(--text-muted)' }}>
              {i + 1}
            </span>
            <span dangerouslySetInnerHTML={{ __html: colorize(line) }} />
          </div>
        ))}
      </code>
    </pre>
  );
}

// ── HTTP status badge ─────────────────────────────────────────────────────────
function StatusBadge({ status }: { status: number }) {
  const ok  = status >= 200 && status < 300;
  const cfg = ok
    ? { c: '#4ade80', bg: 'rgba(34,197,94,0.12)',   b: 'rgba(34,197,94,0.3)'   }
    : status === 429
    ? { c: '#f87171', bg: 'rgba(248,113,113,0.12)', b: 'rgba(248,113,113,0.3)' }
    : status === 401
    ? { c: '#fb923c', bg: 'rgba(251,146,60,0.12)',  b: 'rgba(251,146,60,0.3)'  }
    : { c: '#f43f5e', bg: 'rgba(244,63,94,0.12)',   b: 'rgba(244,63,94,0.3)'   };
  const label = status === 0 ? 'Network Error'
    : status === 429 ? '429 Rate Limited'
    : status === 401 ? '401 Unauthorized'
    : `${status}`;
  return (
    <span className="text-xs font-bold px-3 py-1 rounded-full"
      style={{ color: cfg.c, background: cfg.bg, border: `1px solid ${cfg.b}` }}>
      {label}
    </span>
  );
}

// ── Live rate-limit progress bar ──────────────────────────────────────────────
function RateLimitPanel({ result }: { result: ActionResponse | null }) {
  const hasData = result !== null && result.rateLimitLimit !== null;
  const limit     = result?.rateLimitLimit     ?? 5;
  const remaining = result?.rateLimitRemaining ?? 5;
  const used      = limit - remaining;
  const pct       = hasData ? Math.min((used / limit) * 100, 100) : 0;
  const barColor  = pct >= 90 ? '#f43f5e' : pct >= 70 ? '#f59e0b' : undefined;

  return (
    <section
      id="live-rate-limit"
      className="card-glow fade-up rounded-2xl p-6 flex flex-col gap-5"
      style={{ background: 'var(--bg-card)', animationDelay: '100ms' }}
    >
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-base font-semibold" style={{ color: 'var(--text-primary)' }}>
            Live Rate Limit
          </h2>
          <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
            {hasData ? 'From last API call headers' : 'Run a test to see live data'}
          </p>
        </div>
        <span
          className="flex items-center gap-1.5 text-xs font-medium"
          style={{ color: hasData ? 'var(--teal)' : 'var(--text-muted)' }}
        >
          <span
            className={`w-1.5 h-1.5 rounded-full ${hasData ? 'bg-teal-400 pulse-dot' : 'bg-gray-600'}`}
          />
          {hasData ? 'Live' : 'Idle'}
        </span>
      </div>

      {/* Big numbers */}
      <div className="flex items-end gap-2">
        <span className="text-4xl font-bold tabular-nums" style={{ color: 'var(--text-primary)' }}>
          {hasData ? used : '—'}
        </span>
        <span className="text-base mb-1" style={{ color: 'var(--text-muted)' }}>
          / {hasData ? limit : '—'} requests
        </span>
      </div>

      {/* Progress bar */}
      <div className="flex flex-col gap-2">
        <div
          className="h-3 w-full rounded-full overflow-hidden"
          style={{ background: 'var(--bg-elevated)' }}
          role="progressbar"
          aria-valuenow={used}
          aria-valuemin={0}
          aria-valuemax={limit}
        >
          <div
            className={`h-full rounded-full transition-all duration-700 ${!barColor && hasData ? 'shimmer' : ''}`}
            style={{
              width: `${pct.toFixed(1)}%`,
              background: barColor ?? (hasData ? undefined : 'var(--bg-elevated)'),
              boxShadow: hasData ? (barColor ? `0 0 10px ${barColor}66` : '0 0 12px rgba(99,102,241,0.5)') : 'none',
              minWidth: hasData && pct > 0 ? '8px' : '0',
            }}
          />
        </div>
        <div className="flex justify-between text-xs" style={{ color: 'var(--text-muted)' }}>
          <span>{hasData ? `${pct.toFixed(0)}% used` : 'No data yet'}</span>
          <span>{hasData ? `${remaining} remaining` : ''}</span>
        </div>
      </div>

      {/* Header breakdown */}
      {hasData && (
        <div
          className="rounded-xl p-4 flex flex-col gap-2 font-mono text-xs"
          style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)' }}
        >
          <p style={{ color: 'var(--text-muted)' }}>Response headers</p>
          <div className="flex justify-between">
            <span style={{ color: 'var(--accent-light)' }}>X-Ratelimit-Limit</span>
            <span style={{ color: '#60a5fa' }}>{limit}</span>
          </div>
          <div className="flex justify-between">
            <span style={{ color: 'var(--accent-light)' }}>X-Ratelimit-Remaining</span>
            <span style={{ color: remaining === 0 ? '#f87171' : '#4ade80' }}>{remaining}</span>
          </div>
        </div>
      )}

      {/* Empty state hint */}
      {!hasData && (
        <div
          className="rounded-xl p-4 text-center text-xs"
          style={{
            background: 'var(--bg-elevated)',
            border: '1px dashed var(--border-bright)',
            color: 'var(--text-muted)',
          }}
        >
          Rate limit headers will appear here after your first API call ↑
        </div>
      )}
    </section>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────
export default function DashboardPage() {
  const [apiKey,  setApiKey]  = useState('');
  const [showKey, setShowKey] = useState(false);
  const [query,   setQuery]   = useState('Hinjewadi');
  const [result,  setResult]  = useState<ActionResponse | null>(null);
  const [copied,  setCopied]  = useState(false);
  const [isPending, startTransition] = useTransition();
  const resultsRef = useRef<HTMLDivElement>(null);

  function handleTest(e: React.FormEvent) {
    e.preventDefault();
    startTransition(async () => {
      const res = await searchGeoData(apiKey, query);
      setResult(res);
      setTimeout(() => resultsRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' }), 150);
    });
  }

  function handleCopy() {
    if (!result) return;
    navigator.clipboard.writeText(JSON.stringify(result.data ?? { error: result.error }, null, 2));
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  const hasResult = result !== null;
  const isSuccess = hasResult && result.httpStatus >= 200 && result.httpStatus < 300;

  return (
    <div className="flex min-h-screen" style={{ background: 'var(--bg-base)' }}>
      <Sidebar />

      <div className="flex-1 flex flex-col min-w-0 overflow-x-hidden">
        {/* ── Header ── */}
        <header
          className="flex items-center justify-between px-8 py-4 sticky top-0 z-10 backdrop-blur-md"
          style={{ background: 'rgba(10,12,18,0.85)', borderBottom: '1px solid var(--border)' }}
        >
          <div>
            <h1 className="text-xl font-bold" style={{ color: 'var(--text-primary)' }}>
              GeoData API Platform
            </h1>
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              India's complete LGD-coded geographic hierarchy · REST API
            </p>
          </div>
          <div className="flex items-center gap-3">
            <span className="hidden md:flex items-center gap-1.5 text-xs" style={{ color: 'var(--text-muted)' }}>
              <span className="w-1.5 h-1.5 rounded-full bg-green-400 pulse-dot" />
              Go backend · geo-api-7ngv.onrender.com
            </span>
            <button
              id="upgrade-plan-btn"
              className="hidden sm:flex items-center gap-2 text-sm font-medium px-4 py-2 rounded-xl"
              style={{
                background: 'linear-gradient(135deg,#6366f1 0%,#8b5cf6 100%)',
                color: '#fff',
                boxShadow: '0 0 20px rgba(99,102,241,0.3)',
              }}
            >
              ★ Upgrade Plan
            </button>
          </div>
        </header>

        <main className="flex-1 px-8 py-8 flex flex-col gap-8">

          {/* ── KPI cards ── */}
          <StatCards />

          {/* ── API Tester + Rate Limit (2-col) ── */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">

            {/* API Tester */}
            <form
              id="api-tester-form"
              onSubmit={handleTest}
              className="lg:col-span-2 card-glow fade-up rounded-2xl p-6 flex flex-col gap-5"
              style={{ background: 'var(--bg-card)' }}
            >
              <div className="flex items-center justify-between flex-wrap gap-3">
                <div>
                  <h2 className="text-base font-semibold flex items-center gap-2" style={{ color: 'var(--text-primary)' }}>
                    <span
                      className="w-7 h-7 rounded-lg flex items-center justify-center text-base"
                      style={{ background: 'rgba(99,102,241,0.15)', color: 'var(--accent-light)' }}
                    >⚡</span>
                    API Tester
                  </h2>
                  <p className="text-xs mt-0.5" style={{ color: 'var(--text-muted)' }}>
                    Live connection →{' '}
                    <code className="px-1.5 py-0.5 rounded" style={{ background: 'var(--bg-elevated)', color: 'var(--accent-light)' }}>
                      geo-api-7ngv.onrender.com/api/v1/search
                    </code>
                  </p>
                </div>
                {hasResult && <StatusBadge status={result.httpStatus} />}
              </div>

              {/* API Key */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
                  Bearer Token (API Key)
                </label>
                <div
                  className="flex items-center gap-2 rounded-xl px-4 py-3"
                  style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border-bright)' }}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
                    style={{ color: 'var(--text-muted)', flexShrink: 0 }}>
                    <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
                  </svg>
                  <input
                    id="api-key-input"
                    type={showKey ? 'text' : 'password'}
                    value={apiKey}
                    onChange={(e) => setApiKey(e.target.value)}
                    placeholder="Paste your sk_live_… key here"
                    className="flex-1 bg-transparent outline-none text-sm font-mono"
                    style={{ color: 'var(--text-primary)', caretColor: 'var(--accent-light)' }}
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <button
                    type="button"
                    id="toggle-key-visibility"
                    onClick={() => setShowKey((v) => !v)}
                    className="text-xs px-2 py-0.5 rounded-md flex-shrink-0 transition-colors"
                    style={{ color: 'var(--text-muted)', border: '1px solid var(--border)' }}
                  >
                    {showKey ? 'Hide' : 'Show'}
                  </button>
                </div>
                <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                  Get your key from the <span style={{ color: 'var(--accent-light)' }}>seed_keys</span> script output.
                </p>
              </div>

              {/* Search query */}
              <div className="flex flex-col gap-1.5">
                <label className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
                  Search Query (<code className="px-1 rounded" style={{ background: 'var(--bg-elevated)', color: 'var(--accent-light)' }}>?q=</code>)
                </label>
                <div
                  className="flex items-center gap-2 rounded-xl px-4 py-3"
                  style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border-bright)' }}
                >
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
                    style={{ color: 'var(--text-muted)', flexShrink: 0 }}>
                    <circle cx="11" cy="11" r="8" /><line x1="21" y1="21" x2="16.65" y2="16.65" />
                  </svg>
                  <input
                    id="search-query-input"
                    type="text"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="e.g. Hinjewadi, Yelahanka, Kiraoli…"
                    className="flex-1 bg-transparent outline-none text-sm"
                    style={{ color: 'var(--text-primary)', caretColor: 'var(--accent-light)' }}
                    spellCheck={false}
                  />
                </div>
              </div>

              {/* Submit */}
              <button
                id="test-api-btn"
                type="submit"
                disabled={isPending}
                className="w-full flex items-center justify-center gap-2.5 py-3 rounded-xl text-sm font-semibold transition-all"
                style={{
                  background: isPending
                    ? 'rgba(99,102,241,0.5)'
                    : 'linear-gradient(135deg,#6366f1 0%,#8b5cf6 100%)',
                  color: '#fff',
                  boxShadow: isPending ? 'none' : '0 0 24px rgba(99,102,241,0.4)',
                  cursor: isPending ? 'not-allowed' : 'pointer',
                }}
              >
                {isPending ? (
                  <>
                    <svg className="animate-spin" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                    </svg>
                    Calling API…
                  </>
                ) : (
                  <>
                    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
                      <polygon points="5 3 19 12 5 21 5 3" />
                    </svg>
                    Test API
                  </>
                )}
              </button>

              {/* Error banner */}
              {hasResult && !isSuccess && result.error && (
                <div
                  className="rounded-xl px-4 py-3 flex items-start gap-3 text-sm"
                  style={{
                    background: 'rgba(244,63,94,0.08)',
                    border: '1px solid rgba(244,63,94,0.25)',
                    color: '#fda4af',
                  }}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ flexShrink: 0, marginTop: 1 }}>
                    <circle cx="12" cy="12" r="10" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" />
                  </svg>
                  <span>{result.error}</span>
                </div>
              )}
            </form>

            {/* Live Rate Limit */}
            <RateLimitPanel result={result} />
          </div>

          {/* ── Results panel (shown after first call) ── */}
          {hasResult && (
            <section
              ref={resultsRef}
              id="api-results"
              className="card-glow rounded-2xl overflow-hidden fade-up"
              style={{ background: 'var(--bg-card)' }}
            >
              {/* Results header */}
              <div
                className="flex items-center justify-between px-6 py-4 flex-wrap gap-3"
                style={{ borderBottom: '1px solid var(--border)' }}
              >
                <div className="flex items-center gap-3 flex-wrap">
                  <StatusBadge status={result.httpStatus} />
                  {isSuccess && result.data && (
                    <>
                      <span className="text-sm font-medium" style={{ color: 'var(--text-primary)' }}>
                        {result.data.found} result{result.data.found !== 1 ? 's' : ''} found
                      </span>
                      <span
                        className="text-xs px-2 py-0.5 rounded-full"
                        style={{ background: 'rgba(20,184,166,0.12)', color: '#2dd4bf', border: '1px solid rgba(20,184,166,0.2)' }}
                      >
                        {result.data.search_time_ms} ms
                      </span>
                    </>
                  )}
                </div>
                <div className="flex items-center gap-2">
                  <span className="text-xs font-mono" style={{ color: 'var(--text-muted)' }}>
                    GET /api/v1/search?q={query}
                  </span>
                  <button
                    id="copy-results-btn"
                    type="button"
                    onClick={handleCopy}
                    className="flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg font-medium transition-all"
                    style={{
                      background: copied ? 'rgba(34,197,94,0.12)' : 'rgba(99,102,241,0.12)',
                      color: copied ? '#4ade80' : 'var(--accent-light)',
                      border: `1px solid ${copied ? 'rgba(34,197,94,0.3)' : 'rgba(99,102,241,0.3)'}`,
                    }}
                  >
                    {copied ? '✓ Copied' : '⎘ Copy JSON'}
                  </button>
                </div>
              </div>

              {/* JSON output */}
              <div className="p-6">
                <JsonViewer data={isSuccess ? result.data : { error: result.error, status: result.httpStatus }} />
              </div>
            </section>
          )}

          {/* ── Static sections ── */}
          <ActivityFeed />

          <footer
            className="text-center text-xs py-4"
            style={{ color: 'var(--text-muted)', borderTop: '1px solid var(--border)' }}
          >
            GeoData API Platform · v1.0.0 · © {new Date().getFullYear()} ·{' '}
            <a href="#" style={{ color: 'var(--accent-light)' }}>Docs</a> ·{' '}
            <a href="#" style={{ color: 'var(--accent-light)' }}>Status</a>
          </footer>
        </main>
      </div>
    </div>
  );
}
