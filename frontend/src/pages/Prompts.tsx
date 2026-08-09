import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, Category, PageResult, Prompt } from '../api'

export default function Prompts() {
  const [cats, setCats] = useState<Category[]>([])
  const [prompts, setPrompts] = useState<PageResult<Prompt>>({ items: [], total: 0, page: 1, pageSize: 20 })
  const [keyword, setKeyword] = useState('')
  const [catFilter, setCatFilter] = useState('')
  const [newCat, setNewCat] = useState({ name: '', slug: '' })

  function load() {
    const q = new URLSearchParams()
    if (keyword) q.set('keyword', keyword)
    if (catFilter) q.set('category', catFilter)
    api.get<PageResult<Prompt>>('/api/v1/prompts?' + q.toString()).then(setPrompts).catch(() => {})
  }

  useEffect(() => {
    api.get<Category[]>('/api/v1/prompt-categories').then(setCats).catch(() => {})
  }, [])

  useEffect(() => { load() }, [catFilter])

  async function addCat(e: React.FormEvent) {
    e.preventDefault()
    try {
      await api.post('/api/v1/prompt-categories', { name: newCat.name, slug: newCat.slug })
      setNewCat({ name: '', slug: '' })
      setCats(await api.get<Category[]>('/api/v1/prompt-categories'))
    } catch (err: any) { alert(err.message) }
  }

  return (
    <div>
      <div className="row-between">
        <h1>提示词</h1>
        <Link className="button primary" to="/prompts/new">新建提示词</Link>
      </div>
      <div className="row gap">
        <input placeholder="搜索关键字" value={keyword} onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && load()} />
        <select value={catFilter} onChange={(e) => setCatFilter(e.target.value)}>
          <option value="">全部分类</option>
          {cats.filter((c) => !c.archived).map((c) => <option key={c.id} value={String(c.id)}>{c.icon} {c.name}</option>)}
        </select>
        <button className="primary" onClick={load}>搜索</button>
      </div>
      <div className="split">
        <div>
          <h3>分类</h3>
          <ul className="cat-list">
            {cats.filter((c) => !c.archived).map((c) => (
              <li key={c.id}>
                {c.icon} {c.name} <span className="muted">v{c.schema?.version ?? '-'}</span>
              </li>
            ))}
          </ul>
          <form className="row gap" onSubmit={addCat}>
            <input placeholder="名称" value={newCat.name} onChange={(e) => setNewCat({ ...newCat, name: e.target.value })} />
            <input placeholder="slug" value={newCat.slug} onChange={(e) => setNewCat({ ...newCat, slug: e.target.value })} />
            <button className="primary">新增</button>
          </form>
        </div>
        <table>
          <thead><tr><th>标题</th><th>Slug</th><th>分类</th><th>状态</th><th>版本</th><th></th></tr></thead>
          <tbody>
            {prompts.items.map((p) => (
              <tr key={p.id}>
                <td>{p.title}</td>
                <td>{p.slug}</td>
                <td>{p.categoryName}</td>
                <td>{p.status}</td>
                <td>{p.currentVersion?.version ?? '-'}</td>
                <td><Link to={`/prompts/${p.id}`}>编辑</Link></td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
