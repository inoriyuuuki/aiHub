import { useEffect, useState } from 'react'
import { api, APIToken } from '../api'

export default function Settings() {
  const [oldPassword, setOldPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [tokens, setTokens] = useState<APIToken[]>([])
  const [newToken, setNewToken] = useState({ name: '', scopes: 'read' })
  const [createdToken, setCreatedToken] = useState('')
  const [msg, setMsg] = useState('')

  function load() {
    api.get<APIToken[]>('/api/v1/tokens').then(setTokens).catch(() => {})
  }
  useEffect(() => { load() }, [])

  async function changePassword(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api.post('/api/v1/auth/password', { oldPassword, newPassword })
      setOldPassword(''); setNewPassword('')
      setMsg('密码已修改，其它会话与 Token 已撤销')
    } catch (err: any) { setMsg(err.message) }
  }

  async function createToken(e: React.FormEvent) {
    e.preventDefault()
    try {
      const r = await api.post<{ token: string }>('/api/v1/tokens', {
        name: newToken.name,
        scopes: newToken.scopes.split(',').map((s) => s.trim()),
      })
      setCreatedToken(r.token)
      setNewToken({ name: '', scopes: 'read' })
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  async function revoke(id: number) {
    try {
      await api.del(`/api/v1/tokens/${id}`)
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  return (
    <div>
      <h1>安全设置</h1>
      {msg && <div className="info">{msg}</div>}
      {createdToken && (
        <div className="panel">
          <strong>请立即保存此 Token（只显示一次）：</strong>
          <pre className="mono">{createdToken}</pre>
          <button className="link" onClick={() => setCreatedToken('')}>关闭</button>
        </div>
      )}
      <h3>修改密码</h3>
      <form className="row gap" onSubmit={changePassword}>
        <input type="password" placeholder="原密码" value={oldPassword} onChange={(e) => setOldPassword(e.target.value)} />
        <input type="password" placeholder="新密码（至少 8 位）" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
        <button className="primary">修改</button>
      </form>
      <h3>API Token</h3>
      <form className="row gap" onSubmit={createToken}>
        <input placeholder="名称" value={newToken.name} onChange={(e) => setNewToken({ ...newToken, name: e.target.value })} />
        <input placeholder="scopes(逗号分隔)" value={newToken.scopes} onChange={(e) => setNewToken({ ...newToken, scopes: e.target.value })} />
        <button className="primary">创建</button>
      </form>
      <table>
        <thead><tr><th>名称</th><th>Scopes</th><th>创建时间</th><th>过期</th><th></th></tr></thead>
        <tbody>
          {tokens.map((t) => (
            <tr key={t.id}>
              <td>{t.name}</td>
              <td>{t.scopes.join(',')}</td>
              <td>{new Date(t.createdAt).toLocaleString()}</td>
              <td>{t.expiresAt ? new Date(t.expiresAt).toLocaleString() : '永不过期'}</td>
              <td>{t.revoked ? '已撤销' : <button className="link danger" onClick={() => revoke(t.id)}>撤销</button>}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
