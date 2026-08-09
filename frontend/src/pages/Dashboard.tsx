import { useEffect, useState } from 'react'
import { api, PageResult, Prompt, Skill } from '../api'

export default function Dashboard() {
  const [counts, setCounts] = useState({ prompts: 0, skills: 0 })
  useEffect(() => {
    Promise.all([
      api.get<PageResult<Prompt>>('/api/v1/prompts?pageSize=1'),
      api.get<PageResult<Skill>>('/api/v1/skills?pageSize=1'),
    ])
      .then(([p, s]) => setCounts({ prompts: p.total, skills: s.total }))
      .catch(() => {})
  }, [])
  return (
    <div>
      <h1>仪表盘</h1>
      <div className="cards">
        <div className="card"><div className="num">{counts.prompts}</div><div>提示词</div></div>
        <div className="card"><div className="num">{counts.skills}</div><div>Skills</div></div>
      </div>
      <p className="muted">AIHub 管理提示词、Skill、专家包与 MCP 配置。系统不调用任何 AI 模型。</p>
    </div>
  )
}
