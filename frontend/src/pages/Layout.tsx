import { ReactNode } from 'react'
import { NavLink, useNavigate } from 'react-router-dom'
import { api, ModuleInfo } from '../api'

export default function Layout({ user, modules, onLogout, children }: {
  user: { id: number; username: string; isAdmin: boolean }
  modules: ModuleInfo[]
  onLogout: () => void
  children: ReactNode
}) {
  const nav = useNavigate()
  const enabled = modules.filter((m) => m.enabled)
  const menu: { to: string; label: string; key: string }[] = [
    { to: '/', label: '仪表盘', key: 'dashboard' },
    { to: '/prompts', label: '提示词', key: 'prompts' },
    { to: '/skills', label: 'Skills', key: 'skills' },
    { to: '/experts', label: '专家包', key: 'experts' },
    { to: '/mcp', label: 'MCP 目录', key: 'mcp_catalog' },
    { to: '/settings', label: '安全设置', key: 'settings' },
  ]

  async function logout() {
    try { await api.post('/api/v1/auth/logout') } catch { /* ignore */ }
    onLogout()
    nav('/')
  }

  return (
    <div className="app">
      <aside className="sidebar">
        <div className="brand">AIHub</div>
        <nav>
          {menu.map((m) => {
            const mod = enabled.find((x) => x.id === m.key)
            if (m.key !== 'dashboard' && m.key !== 'settings' && !mod) return null
            return (
              <NavLink key={m.to} to={m.to} className={({ isActive }) => (isActive ? 'active' : '')}>
                {m.label}
              </NavLink>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <span>{user.username}</span>
          <button className="link" onClick={logout}>退出</button>
        </div>
      </aside>
      <main className="content">{children}</main>
    </div>
  )
}
