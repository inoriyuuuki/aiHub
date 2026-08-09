import { useEffect, useState } from 'react'
import { Navigate, Route, Routes } from 'react-router-dom'
import { AUTH_EXPIRED_EVENT, ApiError, api, ModuleInfo } from './api'
import Login from './pages/Login'
import Layout from './pages/Layout'
import Dashboard from './pages/Dashboard'
import Prompts from './pages/Prompts'
import PromptEditor from './pages/PromptEditor'
import Skills from './pages/Skills'
import Experts from './pages/Experts'
import MCP from './pages/MCP'
import Settings from './pages/Settings'

export default function App() {
  const [user, setUser] = useState<{ id: number; username: string; isAdmin: boolean } | null>(null)
  const [modules, setModules] = useState<ModuleInfo[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const onAuthExpired = () => setUser(null)
    window.addEventListener(AUTH_EXPIRED_EVENT, onAuthExpired)
    api
      .get<{ id: number; username: string; isAdmin: boolean }>('/api/v1/auth/me')
      .then((u) => {
        setUser(u)
        return api.get<ModuleInfo[]>('/api/v1/modules')
      })
      .then(setModules)
      .catch((err) => {
        // Only a genuine 401 means logged out; other errors keep the shell usable.
        if (err instanceof ApiError && err.status === 401) setUser(null)
      })
      .finally(() => setLoading(false))
    return () => window.removeEventListener(AUTH_EXPIRED_EVENT, onAuthExpired)
  }, [])

  if (loading) return <div className="center">加载中…</div>
  if (!user) return <Login onLogin={(u, mods) => { setUser(u); setModules(mods) }} />

  return (
    <Layout user={user} modules={modules} onLogout={() => setUser(null)}>
      <Routes>
        <Route path="/" element={<Dashboard />} />
        <Route path="/prompts" element={<Prompts />} />
        <Route path="/prompts/new" element={<PromptEditor />} />
        <Route path="/prompts/:id" element={<PromptEditor />} />
        <Route path="/skills" element={<Skills />} />
        <Route path="/experts" element={<Experts />} />
        <Route path="/mcp" element={<MCP />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </Layout>
  )
}
