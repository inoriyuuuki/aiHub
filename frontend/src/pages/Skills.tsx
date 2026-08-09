import { useEffect, useRef, useState } from 'react'
import { api, PageResult, Skill } from '../api'

export default function Skills() {
  const [skills, setSkills] = useState<PageResult<Skill>>({ items: [], total: 0, page: 1, pageSize: 20 })
  const [keyword, setKeyword] = useState('')
  const fileRef = useRef<HTMLInputElement>(null)
  const [meta, setMeta] = useState({ slug: '', name: '', description: '', category: '', tags: '', changelog: '' })
  const [msg, setMsg] = useState('')

  function load() {
    const q = new URLSearchParams()
    if (keyword) q.set('keyword', keyword)
    api.get<PageResult<Skill>>('/api/v1/skills?' + q.toString()).then(setSkills).catch(() => {})
  }
  useEffect(() => { load() }, [])

  async function upload(f: File) {
    if (!f) return
    const fd = new FormData()
    fd.append('file', f)
    for (const [k, v] of Object.entries(meta)) if (v) fd.append(k, v)
    try {
      const resp = await fetch('/api/v1/skills/upload', { method: 'POST', body: fd, credentials: 'same-origin' })
      const data = await resp.json()
      if (!resp.ok) throw new Error(data?.error?.message || '上传失败')
      setMsg(`已发布 Skill ${data.data.slug}`)
      load()
      if (fileRef.current) fileRef.current.value = ''
    } catch (err: any) { setMsg(err.message) }
  }

  async function manifest(slug: string) {
    try {
      const r = await api.get<{ source: string; downloadUrl: string }>(`/api/v1/skills/install-manifest?slug=${slug}`)
      alert(`安装清单:\n来源: ${r.source}\n下载: ${r.downloadUrl}`)
    } catch (err: any) { alert(err.message) }
  }

  return (
    <div>
      <h1>Skills</h1>
      {msg && <div className="info">{msg}</div>}
      <div className="row gap">
        <input placeholder="搜索" value={keyword} onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} />
        <button className="primary" onClick={load}>搜索</button>
      </div>
      <details className="panel">
        <summary>发布 Skill 压缩包</summary>
        <div className="row gap wrap">
          <input placeholder="slug" value={meta.slug} onChange={(e) => setMeta({ ...meta, slug: e.target.value })} />
          <input placeholder="name" value={meta.name} onChange={(e) => setMeta({ ...meta, name: e.target.value })} />
          <input placeholder="description" value={meta.description} onChange={(e) => setMeta({ ...meta, description: e.target.value })} />
          <input placeholder="category" value={meta.category} onChange={(e) => setMeta({ ...meta, category: e.target.value })} />
          <input placeholder="tags(逗号分隔)" value={meta.tags} onChange={(e) => setMeta({ ...meta, tags: e.target.value })} />
          <input placeholder="changelog" value={meta.changelog} onChange={(e) => setMeta({ ...meta, changelog: e.target.value })} />
          <input ref={fileRef} type="file" accept=".zip" onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
        </div>
      </details>
      <table>
        <thead><tr><th>名称</th><th>Slug</th><th>分类</th><th>版本</th><th>状态</th><th></th></tr></thead>
        <tbody>
          {skills.items.map((s) => (
            <tr key={s.id}>
              <td>{s.name}</td>
              <td>{s.slug}</td>
              <td>{s.category}</td>
              <td>v{s.currentVersion?.version ?? '-'}</td>
              <td>{s.status}</td>
              <td><button className="link" onClick={() => manifest(s.slug)}>安装清单</button></td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
