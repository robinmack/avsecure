import { useState, useEffect } from 'react';
import { BrowserRouter, Route, Routes } from 'react-router-dom';
import IndexPage from './components/IndexPage';
import Room from './components/Rooms';
import './App.css';

const API = 'https://avsecure.vip:8443';

function fmtDuration(seconds) {
  if (!seconds) return '—';
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function fmtNum(n) {
  return (n ?? 0).toLocaleString();
}

function StatRow({ label, count, avg }) {
  return (
    <div className="flex items-center justify-between py-2 border-b border-gray-100 dark:border-gray-800 last:border-0">
      <span className="text-sm text-gray-600 dark:text-gray-400">{label}</span>
      <div className="flex items-center gap-6">
        <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 w-16 text-right">{fmtNum(count)}</span>
        <span className="text-sm text-gray-500 dark:text-gray-500 w-20 text-right">{fmtDuration(avg)}</span>
      </div>
    </div>
  );
}

function StatsModal({ onClose }) {
  const [stats, setStats] = useState(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    fetch(`${API}/stats/public`)
      .then(r => r.json())
      .then(setStats)
      .catch(() => setError(true));
  }, []);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      {/* Backdrop */}
      <div className="absolute inset-0 bg-black/50 backdrop-blur-sm" onClick={onClose} />

      {/* Panel */}
      <div className="relative w-full max-w-md rounded-2xl bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-800 shadow-2xl p-6 flex flex-col gap-5">

        {/* Header */}
        <div className="flex items-start justify-between">
          <div>
            <h2 className="font-semibold text-lg text-gray-900 dark:text-gray-100">Usage statistics</h2>
            <p className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">
              All the information we collect — all anonymous
            </p>
          </div>
          <button onClick={onClose} className="p-1.5 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-gray-400">
            <svg className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12"/>
            </svg>
          </button>
        </div>

        {error && (
          <p className="text-sm text-red-500">Could not load stats.</p>
        )}

        {!stats && !error && (
          <div className="flex justify-center py-8">
            <div className="h-6 w-6 rounded-full border-2 border-blue-500 border-t-transparent animate-spin" />
          </div>
        )}

        {stats && (
          <>
            {/* Headline numbers */}
            <div className="grid grid-cols-3 gap-3">
              {[
                { label: 'Visitors',      value: fmtNum(stats.visits) },
                { label: 'Rooms created', value: fmtNum(stats.rooms_created) },
                { label: 'Total minutes', value: fmtNum(stats.total_minutes) },
              ].map(({ label, value }) => (
                <div key={label} className="flex flex-col items-center p-3 rounded-xl bg-gray-50 dark:bg-gray-800">
                  <span className="text-xl font-bold text-gray-900 dark:text-gray-100">{value}</span>
                  <span className="text-xs text-gray-400 mt-0.5 text-center">{label}</span>
                </div>
              ))}
            </div>

            {/* Chats table */}
            <div>
              <div className="flex items-center justify-between mb-1 px-0">
                <span className="text-xs font-medium text-gray-400 uppercase tracking-wide">Period</span>
                <div className="flex gap-6">
                  <span className="text-xs font-medium text-gray-400 uppercase tracking-wide w-16 text-right">Chats</span>
                  <span className="text-xs font-medium text-gray-400 uppercase tracking-wide w-20 text-right">Avg duration</span>
                </div>
              </div>
              <div className="rounded-xl border border-gray-100 dark:border-gray-800 px-3 divide-y divide-gray-100 dark:divide-gray-800">
                <StatRow label="This week"  count={stats.chats_week}  avg={stats.avg_dur_week}  />
                <StatRow label="This month" count={stats.chats_month} avg={stats.avg_dur_month} />
                <StatRow label="This year"  count={stats.chats_year}  avg={stats.avg_dur_year}  />
                <StatRow label="All time"   count={stats.chats_total} avg={stats.avg_dur_all}   />
              </div>
            </div>

            {/* Security blurb */}
            <div className="rounded-xl bg-blue-50 dark:bg-blue-950/40 border border-blue-100 dark:border-blue-900 p-4">
              <p className="text-xs font-semibold text-blue-700 dark:text-blue-400 mb-1">🔒 How your call is protected</p>
              <p className="text-xs text-blue-600 dark:text-blue-500 leading-relaxed">
                All video and audio is encrypted with <strong>DTLS-SRTP</strong> — the same standard used by Signal and WhatsApp.
                Encryption keys are negotiated directly between browsers and never touch our server.
                We store no video, audio, IP addresses, or room IDs. The only data we record is call count and duration.
              </p>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

function App() {
  const [dark, setDark] = useState(() => localStorage.getItem('theme') !== 'light');
  const [showStats, setShowStats] = useState(false);

  useEffect(() => {
    const root = document.documentElement;
    if (dark) { root.classList.add('dark');    localStorage.setItem('theme', 'dark'); }
    else       { root.classList.remove('dark'); localStorage.setItem('theme', 'light'); }
  }, [dark]);

  // Record visit once per page load
  useEffect(() => {
    fetch(`${API}/stats/visit`, { method: 'POST', mode: 'cors' }).catch(() => {});
  }, []);

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-950 text-gray-900 dark:text-gray-100 transition-colors duration-300">
      <header className="border-b border-gray-200 dark:border-gray-800 bg-white dark:bg-gray-900">
        <div className="max-w-5xl mx-auto px-6 py-4 flex items-center justify-between">
          <div className="flex items-center gap-2">
            <span className="text-2xl">🔒</span>
            <span className="font-semibold text-lg tracking-tight">AVSecure</span>
            <span className="text-xs font-medium px-2 py-0.5 rounded-full bg-blue-100 dark:bg-blue-900 text-blue-700 dark:text-blue-300 ml-1">BETA</span>
          </div>
          <div className="flex items-center gap-2">
            {/* Stats button */}
            <button
              onClick={() => setShowStats(true)}
              className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors text-gray-500 dark:text-gray-400"
              aria-label="Usage statistics"
              title="Usage statistics"
            >
              <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/>
              </svg>
            </button>

            {/* Theme toggle */}
            <button
              onClick={() => setDark(d => !d)}
              className="p-2 rounded-lg hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
              aria-label="Toggle theme"
            >
              {dark ? (
                <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5 text-yellow-400" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M12 3a1 1 0 0 1 1 1v1a1 1 0 1 1-2 0V4a1 1 0 0 1 1-1zm0 15a1 1 0 0 1 1 1v1a1 1 0 1 1-2 0v-1a1 1 0 0 1 1-1zm9-7a1 1 0 0 1-1 1h-1a1 1 0 1 1 0-2h1a1 1 0 0 1 1 1zM4 12a1 1 0 0 1-1 1H2a1 1 0 1 1 0-2h1a1 1 0 0 1 1 1zm14.95-6.364a1 1 0 0 1 0 1.414l-.707.707a1 1 0 1 1-1.414-1.414l.707-.707a1 1 0 0 1 1.414 0zM6.757 17.657a1 1 0 0 1 0 1.414l-.707.707a1 1 0 1 1-1.414-1.414l.707-.707a1 1 0 0 1 1.414 0zM18.95 18.364a1 1 0 0 1-1.414 0l-.707-.707a1 1 0 1 1 1.414-1.414l.707.707a1 1 0 0 1 0 1.414zM6.757 6.343a1 1 0 0 1-1.414 0l-.707-.707A1 1 0 0 1 6.05 4.222l.707.707a1 1 0 0 1 0 1.414zM12 7a5 5 0 1 0 0 10A5 5 0 0 0 12 7z"/>
                </svg>
              ) : (
                <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5 text-gray-600" viewBox="0 0 24 24" fill="currentColor">
                  <path d="M21 12.79A9 9 0 1 1 11.21 3a7 7 0 0 0 9.79 9.79z"/>
                </svg>
              )}
            </button>
          </div>
        </div>
      </header>

      <main className="max-w-5xl mx-auto px-6 py-10">
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<IndexPage />} />
            <Route path="/room/:roomID" element={<Room />} />
          </Routes>
        </BrowserRouter>
      </main>

      {showStats && <StatsModal onClose={() => setShowStats(false)} />}
    </div>
  );
}

export default App;
