import { FormEvent, useState } from 'react'
import { api, ModuleInfo } from '../api'

export default function Login({ onLogin }: {
  onLogin: (u: { id: number; username: string; isAdmin: boolean }, mods: ModuleInfo[]) => void
}) {
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const u = await api.post<{ user: { id: number; username: string; isAdmin: boolean } }>(
        '/api/v1/auth/login', { username, password },
      )
      const mods = await api.get<ModuleInfo[]>('/api/v1/modules')
      onLogin(u.user, mods)
    } catch (err: any) {
      setError(err.message || '登录失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="login-wrap">
      <form className="login-card" onSubmit={submit}>
        <h1>AIHub</h1>
        <p className="muted">AI 资产管理平台</p>
        <input placeholder="用户名" value={username} onChange={(e) => setUsername(e.target.value)} autoFocus />
        <input placeholder="密码" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
        {error && <div className="error">{error}</div>}
        <button className="primary" disabled={busy}>{busy ? '登录中…' : '登录'}</button>
      </form>
    </div>
  )
}
