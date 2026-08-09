import { useEffect, useState } from 'react'
import { api, MCPDefinition, MCPProfile, MCPProfileItem } from '../api'

export default function MCP() {
  const [defs, setDefs] = useState<MCPDefinition[]>([])
  const [profiles, setProfiles] = useState<MCPProfile[]>([])
  const [selProfile, setSelProfile] = useState<MCPProfile | null>(null)
  const [items, setItems] = useState<MCPProfileItem[]>([])
  const [newDef, setNewDef] = useState({ name: '', slug: '', transport: 'stdio' })
  const [newProfile, setNewProfile] = useState({ name: '', slug: '', scope: 'global' })
  const [newVer, setNewVer] = useState<{ command: string; args: string; url: string; envVars: string; tools: string }>({ command: '', args: '', url: '', envVars: '', tools: '' })
  const [addDefId, setAddDefId] = useState(0)
  const [msg, setMsg] = useState('')

  function load() {
    api.get<MCPDefinition[]>('/api/v1/mcp/definitions').then(setDefs).catch(() => {})
    api.get<MCPProfile[]>('/api/v1/mcp/profiles').then(setProfiles).catch(() => {})
  }
  useEffect(() => { load() }, [])

  async function createDef(e: React.FormEvent) {
    e.preventDefault()
    try {
      const d = await api.post<MCPDefinition>('/api/v1/mcp/definitions', newDef)
      setDefs(await api.get('/api/v1/mcp/definitions'))
      // publish initial version
      const config: Record<string, any> = {}
      if (newDef.transport === 'stdio') { config.command = 'npx'; config.args = ['-y', d.slug] }
      else config.url = 'https://example.com/mcp'
      await api.post(`/api/v1/mcp/definitions/${d.id}/versions`, { config, envVars: [], tools: [] })
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  async function createProfile(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api.post('/api/v1/mcp/profiles', newProfile)
      setNewProfile({ name: '', slug: '', scope: 'global' })
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  async function openProfile(p: MCPProfile) {
    setSelProfile(p)
    setItems(await api.get(`/api/v1/mcp/profiles/${p.id}/items`))
  }

  async function addItem() {
    if (!selProfile || !addDefId) return
    const d = defs.find((x) => x.id === addDefId)
    if (!d?.currentVersion) { setMsg('该定义没有已发布版本'); return }
    try {
      await api.post(`/api/v1/mcp/profiles/${selProfile.id}/items`, {
        definitionId: d.id,
        definitionVersionId: d.currentVersion.id,
        enabledTools: [],
        disabledTools: [],
      })
      setItems(await api.get(`/api/v1/mcp/profiles/${selProfile.id}/items`))
    } catch (err: any) { setMsg(err.message) }
  }

  async function publishVersion(defId: number) {
    try {
      const cfg: Record<string, any> = {}
      if (newVer.command) cfg.command = newVer.command
      if (newVer.args) cfg.args = newVer.args.split(',').map((s) => s.trim()).filter(Boolean)
      if (newVer.url) cfg.url = newVer.url
      let envVars: Record<string, any>[] = []
      if (newVer.envVars) envVars = newVer.envVars.split(';').map((s) => {
        const [n, desc] = s.split(':')
        return { name: n.trim(), description: desc?.trim() || '', sensitive: false, required: false }
      }).filter((v) => v.name)
      let tools: Record<string, any>[] = []
      if (newVer.tools) tools = newVer.tools.split(',').map((s) => ({ name: s.trim(), description: '', defaultEnabled: true })).filter((t) => t.name)
      await api.post(`/api/v1/mcp/definitions/${defId}/versions`, { config: cfg, envVars, tools })
      setNewVer({ command: '', args: '', url: '', envVars: '', tools: '' })
      load()
      setMsg('版本已发布')
    } catch (err: any) { setMsg(err.message) }
  }

  async function showManifest(slug: string) {
    try {
      const r = await api.get<any>(`/api/v1/mcp/install-manifest?profile=${slug}`)
      alert(JSON.stringify(r, null, 2))
    } catch (err: any) { alert(err.message) }
  }

  return (
    <div>
      <h1>MCP 目录</h1>
      {msg && <div className="info">{msg}</div>}
      <div className="split">
        <div>
          <h3>MCP 定义</h3>
          <form className="row gap" onSubmit={createDef}>
            <input placeholder="名称" value={newDef.name} onChange={(e) => setNewDef({ ...newDef, name: e.target.value })} />
            <input placeholder="slug" value={newDef.slug} onChange={(e) => setNewDef({ ...newDef, slug: e.target.value })} />
            <select value={newDef.transport} onChange={(e) => setNewDef({ ...newDef, transport: e.target.value })}>
              <option value="stdio">stdio</option><option value="http">http</option>
            </select>
            <button className="primary">新增</button>
          </form>
          <table>
            <thead><tr><th>名称</th><th>Slug</th><th>传输</th><th>版本</th></tr></thead>
            <tbody>
              {defs.map((d) => (
                <tr key={d.id}>
                  <td>{d.name}</td><td>{d.slug}</td><td>{d.transport}</td>
                  <td>v{d.currentVersion?.version ?? '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <details className="panel">
            <summary>发布定义新版本</summary>
            <div className="row gap wrap">
              <select onChange={(e) => setAddDefId(Number(e.target.value))} value={addDefId}>
                <option value={0}>选择定义</option>
                {defs.map((d) => <option key={d.id} value={d.id}>{d.slug}</option>)}
              </select>
              <input placeholder="command" value={newVer.command} onChange={(e) => setNewVer({ ...newVer, command: e.target.value })} />
              <input placeholder="args(逗号分隔)" value={newVer.args} onChange={(e) => setNewVer({ ...newVer, args: e.target.value })} />
              <input placeholder="url" value={newVer.url} onChange={(e) => setNewVer({ ...newVer, url: e.target.value })} />
              <input placeholder="env: NAME:说明;KEY2:说明" value={newVer.envVars} onChange={(e) => setNewVer({ ...newVer, envVars: e.target.value })} />
              <input placeholder="tools(逗号分隔)" value={newVer.tools} onChange={(e) => setNewVer({ ...newVer, tools: e.target.value })} />
              <button className="primary" onClick={() => publishVersion(addDefId)}>发布版本</button>
            </div>
          </details>
        </div>
        <div>
          <h3>Profiles</h3>
          <form className="row gap" onSubmit={createProfile}>
            <input placeholder="名称" value={newProfile.name} onChange={(e) => setNewProfile({ ...newProfile, name: e.target.value })} />
            <input placeholder="slug" value={newProfile.slug} onChange={(e) => setNewProfile({ ...newProfile, slug: e.target.value })} />
            <select value={newProfile.scope} onChange={(e) => setNewProfile({ ...newProfile, scope: e.target.value })}>
              <option value="global">global</option><option value="project">project</option>
            </select>
            <button className="primary">新增</button>
          </form>
          <table>
            <thead><tr><th>名称</th><th>Slug</th><th>范围</th><th></th></tr></thead>
            <tbody>
              {profiles.map((p) => (
                <tr key={p.id}>
                  <td>{p.name}</td><td>{p.slug}</td><td>{p.scope}</td>
                  <td>
                    <button className="link" onClick={() => openProfile(p)}>管理</button>
                    <button className="link" onClick={() => showManifest(p.slug)}>清单</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {selProfile && (
            <div>
              <div className="row gap">
                <select value={addDefId} onChange={(e) => setAddDefId(Number(e.target.value))}>
                  <option value={0}>选择 MCP 定义</option>
                  {defs.map((d) => <option key={d.id} value={d.id}>{d.slug} v{d.currentVersion?.version}</option>)}
                </select>
                <button className="primary" onClick={addItem}>添加</button>
              </div>
              <table>
                <thead><tr><th>定义</th><th>版本</th><th>传输</th></tr></thead>
                <tbody>
                  {items.map((it) => (
                    <tr key={it.id}><td>{it.definitionName}</td><td>v{it.version}</td><td>{it.transport}</td></tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
