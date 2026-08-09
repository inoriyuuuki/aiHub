import { useEffect, useState } from 'react'
import { api, ExpertPack, PageResult, Skill } from '../api'

export default function Experts() {
  const [packs, setPacks] = useState<PageResult<ExpertPack>>({ items: [], total: 0, page: 1, pageSize: 20 })
  const [skills, setSkills] = useState<Skill[]>([])
  const [sel, setSel] = useState<ExpertPack | null>(null)
  const [members, setMembers] = useState<any[]>([])
  const [newPack, setNewPack] = useState({ name: '', slug: '', description: '', domain: '', responsibility: '', usage: '' })
  const [addSkillId, setAddSkillId] = useState(0)
  const [msg, setMsg] = useState('')

  function load() {
    api.get<PageResult<ExpertPack>>('/api/v1/expert-packs').then(setPacks).catch(() => {})
  }
  useEffect(() => {
    load()
    api.get<PageResult<Skill>>('/api/v1/skills?pageSize=100').then((r) => setSkills(r.items)).catch(() => {})
  }, [])

  async function create(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api.post('/api/v1/expert-packs', newPack)
      setNewPack({ name: '', slug: '', description: '', domain: '', responsibility: '', usage: '' })
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  async function open(pack: ExpertPack) {
    setSel(pack)
    const m = await api.get<any[]>('/api/v1/expert-packs/' + pack.id + '/members')
    setMembers(m)
  }

  async function addMember() {
    if (!sel || !addSkillId) return
    const skill = skills.find((s) => s.id === addSkillId)
    if (!skill?.currentVersion) { setMsg('该 Skill 没有已发布版本'); return }
    try {
      await api.post(`/api/v1/expert-packs/${sel.id}/members`, {
        skillId: skill.id,
        skillVersionId: skill.currentVersion.id,
      })
      setMembers(await api.get(`/api/v1/expert-packs/${sel.id}/members`))
    } catch (err: any) { setMsg(err.message) }
  }

  async function removeMember(skillId: number) {
    if (!sel) return
    try {
      await api.del(`/api/v1/expert-packs/${sel.id}/members/${skillId}`)
      setMembers(await api.get(`/api/v1/expert-packs/${sel.id}/members`))
    } catch (err: any) { setMsg(err.message) }
  }

  async function build() {
    if (!sel) return
    try {
      const p = await api.post<ExpertPack>(`/api/v1/expert-packs/${sel.id}/build`, {})
      setSel(p)
      setMsg('构建完成')
      load()
    } catch (err: any) { setMsg(err.message) }
  }

  return (
    <div>
      <h1>专家包</h1>
      {msg && <div className="info">{msg}</div>}
      <details className="panel">
        <summary>新建专家包</summary>
        <form className="grid" onSubmit={create}>
          <input placeholder="名称" value={newPack.name} onChange={(e) => setNewPack({ ...newPack, name: e.target.value })} />
          <input placeholder="slug" value={newPack.slug} onChange={(e) => setNewPack({ ...newPack, slug: e.target.value })} />
          <input placeholder="领域" value={newPack.domain} onChange={(e) => setNewPack({ ...newPack, domain: e.target.value })} />
          <input placeholder="职责" value={newPack.responsibility} onChange={(e) => setNewPack({ ...newPack, responsibility: e.target.value })} />
          <input placeholder="使用说明" value={newPack.usage} onChange={(e) => setNewPack({ ...newPack, usage: e.target.value })} />
          <button className="primary">创建</button>
        </form>
      </details>
      <div className="split">
        <table>
          <thead><tr><th>名称</th><th>Slug</th><th>版本</th><th></th></tr></thead>
          <tbody>
            {packs.items.map((p) => (
              <tr key={p.id} className={sel?.id === p.id ? 'row-active' : ''}>
                <td>{p.name}</td><td>{p.slug}</td>
                <td>{p.currentVersion?.version ?? '-'}</td>
                <td><button className="link" onClick={() => open(p)}>管理</button></td>
              </tr>
            ))}
          </tbody>
        </table>
        {sel && (
          <div>
            <h3>{sel.name}</h3>
            <div className="row gap">
              <select value={addSkillId} onChange={(e) => setAddSkillId(Number(e.target.value))}>
                <option value={0}>选择 Skill</option>
                {skills.map((s) => <option key={s.id} value={s.id}>{s.slug} v{s.currentVersion?.version}</option>)}
              </select>
              <button className="primary" onClick={addMember}>添加成员</button>
              <button className="primary" onClick={build}>构建</button>
            </div>
            <table>
              <thead><tr><th>Skill</th><th>版本</th><th>SHA256</th><th></th></tr></thead>
              <tbody>
                {members.map((m) => (
                  <tr key={m.skillId}>
                    <td>{m.skillSlug}</td><td>v{m.version}</td>
                    <td className="mono">{m.sha256.slice(0, 12)}…</td>
                    <td><button className="link danger" onClick={() => removeMember(m.skillId)}>移除</button></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  )
}
