import { Navigate, NavLink, Route, Routes } from 'react-router-dom'
import { Ask } from './pages/Ask'
import { Jobs } from './pages/Jobs'
import { Overview } from './pages/Overview'
import { Profile } from './pages/Profile'
import { Sources } from './pages/Sources'

export function App() {
  return (
    <div className="shell">
      <header className="topbar">
        <NavLink to="/" className="brand" end>
          <span className="brand-mark" aria-hidden="true">
            <svg width="16" height="16" viewBox="0 0 32 32">
              <path d="M9 22 16 8l7 14-7-4z" fill="#fff" />
            </svg>
          </span>
          job-pilot
        </NavLink>
        <nav className="nav" aria-label="Main">
          <NavLink to="/" end>
            Funnel
          </NavLink>
          <NavLink to="/jobs">Jobs</NavLink>
          <NavLink to="/ask">Ask</NavLink>
          <NavLink to="/profile">Profile</NavLink>
          <NavLink to="/sources">Sources</NavLink>
        </nav>
      </header>
      <main>
        <Routes>
          <Route path="/" element={<Overview />} />
          <Route path="/jobs" element={<Jobs />} />
          <Route path="/ask" element={<Ask />} />
          <Route path="/profile" element={<Profile />} />
          <Route path="/sources" element={<Sources />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  )
}
