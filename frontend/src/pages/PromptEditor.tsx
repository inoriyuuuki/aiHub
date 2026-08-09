import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, Category, Prompt, PromptVersion } from '../api'

interface SchemaProp {
  type?: string
  title?: string
  'x-aihub-ui'?: string
  description?: string
  enum?: string[]
  items?: { type?: string; properties?: Record<string, SchemaProp>; title?: string }
  properties?: Record<string, SchemaProp>
  maxLength?: number
  minLength?: number
  minimum?: number
  maximum?: number
}

interface Schema {
  type: string
  properties: Record<string, SchemaProp>
  required?: string[]
  'x-aihub-variables'?: { name: string; description?: string; default?: string }[]
}

export default function PromptEditor() {
  const { id } = useParams()
  const nav = useNavigate()
  const [cats, setCats] = useState<Category[]>([])
  const [prompt, setPrompt] = useState<Prompt | null>(null)
  const [categoryId, setCategoryId] = useState(0)
  const [schema, setSchema] = useState<Schema | null>(null)
  const [form, setForm] = useState<Record<string, any>>({})
  const [slug, setSlug] = useState('')
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [summary, setSummary] = useState('')
  const [versions, setVersions] = useState<PromptVersion[]>([])
  const [diff, setDiff] = useState('')
  const [msg, setMsg] = useState('')

  const promptId = id ? Number(id) : null

  const loadCats = useCallback(async () => {
    const cs = await api.get<Category[]>('/api/v1/prompt-categories')
    setCats(cs.filter((c) => !c.archived))
    return cs
  }, [])

  useEffect(() => {
    loadCats().then((cs) => {
      if (promptId) {
        api.get<Prompt>(`/api/v1/prompts/${promptId}`).then((p) => {
          setPrompt(p)
          setCategoryId(p.categoryId)
          setSlug(p.slug)
          setTitle(p.title)
          setDescription(p.description)
          if (p.draft) setForm(p.draft.content || {})
          const cat = cs.find((c) => c.id === p.categoryId)
          if (cat?.schema) setSchema(cat.schema.schema as Schema)
        }).catch(() => {})
        api.get<PromptVersion[]>(`/api/v1/prompts/${promptId}/versions`).then(setVersions).catch(() => {})
      }
    })
  }, [promptId, loadCats])

  async function pickCategory(cid: number) {
    setCategoryId(cid)
    const c = cats.find((x) => x.id === cid)
    if (c?.schema) {
      setSchema(c.schema.schema as Schema)
      setForm(defaults(c.schema.schema as Schema))
    }
  }

  function setPath(obj: Record<string, any>, path: string[], value: any) {
    const next = { ...obj }
    let cur = next
    for (let i = 0; i < path.length - 1; i++) {
      const key = path[i]
      if (typeof cur[key] !== 'object' || cur[key] === null) cur[key] = {}
      cur = cur[key]
    }
    cur[path[path.length - 1]] = value
    return next
  }

  async function saveDraft() {
    if (!categoryId || !slug || !title) { setMsg('请填写分类、slug 和标题'); return }
    const body: any = { categoryId, slug, title, description, content: form, summary }
    try {
      if (promptId) {
        const p = await api.patch<Prompt>(`/api/v1/prompts/${promptId}`, body)
        setPrompt(p)
        setMsg('草稿已保存')
      } else {
        const p = await api.post<Prompt>('/api/v1/prompts', body)
        setPrompt(p)
        nav(`/prompts/${p.id}`, { replace: true })
        setMsg('草稿已创建')
      }
    } catch (err: any) { setMsg(err.message) }
  }

  async function publish() {
    if (!promptId) { await saveDraft(); return }
    try {
      const p = await api.post<Prompt>(`/api/v1/prompts/${promptId}/publish`, { summary })
      setPrompt(p)
      setVersions(await api.get<PromptVersion[]>(`/api/v1/prompts/${promptId}/versions`))
      setMsg('已发布新版本')
    } catch (err: any) { setMsg(err.message) }
  }

  async function showDiff(v: number) {
    if (!promptId) return
    try {
      const r = await api.get<{ diff: string }>(`/api/v1/prompts/${promptId}/versions/${v}/diff?base=draft`)
      setDiff(r.diff)
    } catch (err: any) { setDiff('无法获取差异: ' + err.message) }
  }

  async function rollback(v: number) {
    if (!promptId) return
    try {
      await api.post(`/api/v1/prompts/${promptId}/rollback`, { version: v })
      setMsg('已回滚')
    } catch (err: any) { setMsg(err.message) }
  }

  return (
    <div>
      <div className="row-between">
        <h1>{promptId ? '编辑提示词' : '新建提示词'}</h1>
        <div className="row gap">
          <button className="primary" onClick={saveDraft}>保存草稿</button>
          <button className="primary" onClick={publish}>发布版本</button>
        </div>
      </div>
      {msg && <div className="info">{msg}</div>}
      <div className="split">
        <div>
          <label>分类</label>
          <select value={categoryId} onChange={(e) => pickCategory(Number(e.target.value))} disabled={!!prompt?.currentVersion}>
            <option value={0}>选择分类</option>
            {cats.map((c) => <option key={c.id} value={c.id}>{c.icon} {c.name}</option>)}
          </select>
          <div className="row gap">
            <div><label>Slug</label><input value={slug} onChange={(e) => setSlug(e.target.value)} /></div>
            <div><label>标题</label><input value={title} onChange={(e) => setTitle(e.target.value)} /></div>
          </div>
          <label>说明</label>
          <input value={description} onChange={(e) => setDescription(e.target.value)} />
          <label>版本说明</label>
          <input value={summary} onChange={(e) => setSummary(e.target.value)} />
          {schema && (
            <div className="form">
              {Object.entries(schema.properties).map(([name, prop]) => (
                <Field
                  key={name}
                  name={name}
                  prop={prop}
                  value={form[name]}
                  path={[name]}
                  setValue={(path, v) => setForm((f) => setPath(f, path, v))}
                  promptId={promptId}
                />
              ))}
            </div>
          )}
        </div>
        <div>
          <h3>版本历史</h3>
          <table>
            <thead><tr><th>版本</th><th>说明</th><th>时间</th><th></th></tr></thead>
            <tbody>
              {versions.map((v) => (
                <tr key={v.id}>
                  <td>v{v.version}</td>
                  <td>{v.summary || '-'}</td>
                  <td>{new Date(v.createdAt).toLocaleString()}</td>
                  <td>
                    <button className="link" onClick={() => showDiff(v.version)}>对比</button>
                    <button className="link" onClick={() => rollback(v.version)}>回滚</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {diff && <pre className="diff">{diff}</pre>}
        </div>
      </div>
    </div>
  )
}

function defaults(schema: Schema): Record<string, any> {
  const out: Record<string, any> = {}
  for (const [name, prop] of Object.entries(schema.properties)) {
    if (prop.type === 'object' && prop.properties) {
      const nested = defaults({ type: 'object', properties: prop.properties } as Schema)
      if (Object.keys(nested).length) out[name] = nested
    } else if (prop.type === 'array' && prop['x-aihub-ui'] === 'repeatable-group') {
      out[name] = [{}]
    } else if (prop.type === 'array') {
      out[name] = []
    } else if (prop.type === 'boolean') {
      out[name] = false
    } else if (prop.type === 'number' || prop.type === 'integer') {
      out[name] = prop.minimum ?? 0
    } else {
      const vars = schema['x-aihub-variables']?.find((v) => v.name === name)
      out[name] = vars?.default ?? ''
    }
  }
  return out
}

function Field({ name, prop, value, path, setValue, promptId, depth = 0 }: {
  name: string
  prop: SchemaProp
  value: any
  path: string[]
  setValue: (path: string[], v: any) => void
  promptId: number | null
  depth?: number
}) {
  const ui = prop['x-aihub-ui'] || 'text'
  const label = prop.title || name

  if (prop.type === 'object' && prop.properties) {
    return (
      <fieldset>
        <legend>{label}</legend>
        {Object.entries(prop.properties).map(([n, p]) => (
          <Field key={n} name={n} prop={p} value={value?.[n]} path={[...path, n]} setValue={setValue} promptId={promptId} depth={depth + 1} />
        ))}
      </fieldset>
    )
  }

  if (prop.type === 'array' && ui === 'repeatable-group' && prop.items?.properties) {
    const items: Record<string, any>[] = Array.isArray(value) && value.length ? value : [{}]
    return (
      <fieldset>
        <legend>{label}</legend>
        {items.map((item, idx) => (
          <div key={idx} className="repeat-item">
            {Object.entries(prop.items!.properties!).map(([n, p]) => (
              <Field key={n} name={n} prop={p} value={item[n]} path={[...path, String(idx), n]} setValue={setValue} promptId={promptId} depth={depth + 1} />
            ))}
            <button className="link danger" onClick={() => {
              const next = items.filter((_, i) => i !== idx)
              setValue(path, next.length ? next : [{}])
            }}>删除</button>
          </div>
        ))}
        <button className="link" onClick={() => setValue(path, [...items, {}])}>+ 添加一项</button>
      </fieldset>
    )
  }

  if (prop.type === 'array' && ui === 'multi-select') {
    const opts = prop.enum || []
    return (
      <div className="field">
        <label>{label}</label>
        <div className="chips">
          {opts.map((o) => {
            const arr: string[] = Array.isArray(value) ? value : []
            const checked = arr.includes(o)
            return (
              <button key={o} type="button" className={checked ? 'chip on' : 'chip'} onClick={() => {
                setValue(path, checked ? arr.filter((x) => x !== o) : [...arr, o])
              }}>{o}</button>
            )
          })}
        </div>
      </div>
    )
  }

  if (['file', 'image', 'effect-file'].includes(ui)) {
    return <AssetField name={name} label={label} value={value} path={path} setValue={setValue} promptId={promptId} kind={ui === 'image' ? 'image' : ui === 'effect-file' ? 'effect-file' : 'file'} />
  }

  if (ui === 'switch') {
    return (
      <label className="field switch">
        <input type="checkbox" checked={!!value} onChange={(e) => setValue(path, e.target.checked)} />
        {label}
      </label>
    )
  }

  const input = (() => {
    if (ui === 'textarea' || ui === 'markdown' || ui === 'code') {
      return <textarea rows={ui === 'code' ? 8 : 4} value={value ?? ''} onChange={(e) => setValue(path, e.target.value)} style={ui === 'code' ? { fontFamily: 'monospace' } : undefined} />
    }
    if (ui === 'number') return <input type="number" value={value ?? ''} onChange={(e) => setValue(path, Number(e.target.value))} />
    if (['select', 'radio', 'model-provider', 'model-name'].includes(ui)) {
      const opts = prop.enum || (ui === 'model-provider' ? ['openai', 'anthropic', 'google', 'deepseek', '其他'] : ui === 'model-name' ? [] : [])
      if (opts.length) {
        return (
          <select value={value ?? ''} onChange={(e) => setValue(path, e.target.value)}>
            <option value="">请选择</option>
            {opts.map((o) => <option key={o} value={o}>{o}</option>)}
          </select>
        )
      }
    }
    return <input value={value ?? ''} onChange={(e) => setValue(path, e.target.value)} />
  })()

  return (
    <div className="field">
      <label>{label}{prop.description ? <span className="muted"> — {prop.description}</span> : null}</label>
      {input}
    </div>
  )
}

function AssetField({ name, label, value, path, setValue, promptId, kind }: {
  name: string
  label: string
  value: any
  path: string[]
  setValue: (path: string[], v: any) => void
  promptId: number | null
  kind: 'image' | 'effect-file' | 'file'
}) {
  const fileRef = useRef<HTMLInputElement>(null)
  const [busy, setBusy] = useState(false)
  const arr: string[] = Array.isArray(value) ? value.map(String) : value ? [String(value)] : []

  async function upload(f: File) {
    if (!promptId) { alert('请先保存草稿后再上传附件'); return }
    setBusy(true)
    try {
      const buf = await f.arrayBuffer()
      const digest = await crypto.subtle.digest('SHA-256', buf)
      const sha256 = [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('')
      const pre = await api.post<{ objectKey: string; uploadUrl: string }>('/api/v1/assets/presign', {
        kind: kind === 'image' ? 'image' : kind === 'effect-file' ? 'effect-file' : 'file',
        filename: f.name,
        size: f.size,
        sha256,
        mime: f.type || 'application/octet-stream',
        refType: 'prompt',
        refId: promptId,
      })
      const put = await fetch(pre.uploadUrl, { method: 'PUT', body: buf, headers: { 'Content-Type': f.type || 'application/octet-stream' } })
      if (!put.ok) throw new Error('上传对象失败')
      const conf = await api.post<{ id: number; objectKey: string }>('/api/v1/assets/confirm', {
        objectKey: pre.objectKey,
        kind: kind === 'image' ? 'image' : kind === 'effect-file' ? 'effect-file' : 'file',
        filename: f.name,
        size: f.size,
        sha256,
        mime: f.type || 'application/octet-stream',
        refType: 'prompt',
        refId: promptId,
      })
      const idStr = String(conf.id)
      if (path[path.length - 1] === name && kind === 'image') {
        // single image field -> keep as scalar; we model as array everywhere
      }
      setValue(path, [...arr, idStr])
    } catch (err: any) {
      alert('上传失败: ' + err.message)
    } finally {
      setBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  return (
    <div className="field">
      <label>{label}</label>
      <div className="asset-list">
        {arr.map((id, i) => <AssetThumb key={id + i} assetId={Number(id)} onRemove={() => setValue(path, arr.filter((_, j) => j !== i))} />)}
        <input ref={fileRef} type="file" accept={kind === 'image' ? 'image/*' : undefined} onChange={(e) => e.target.files?.[0] && upload(e.target.files[0])} />
      </div>
      {busy && <div className="muted">上传中…</div>}
    </div>
  )
}

function AssetThumb({ assetId, onRemove }: { assetId: number; onRemove: () => void }) {
  const [url, setUrl] = useState('')
  useEffect(() => {
    api.get<{ url: string }>(`/api/v1/assets/${assetId}/url`).then((r) => setUrl(r.url)).catch(() => {})
  }, [assetId])
  if (!url) return <span className="asset">#{assetId}</span>
  return (
    <span className="asset">
      <img src={url} alt="" style={{ maxHeight: 48 }} onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }} />
      <button className="link danger" onClick={onRemove}>移除</button>
    </span>
  )
}
