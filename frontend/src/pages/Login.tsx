import { useState } from 'react'
import { login } from '../api'
import { go } from '../components'

export function Login() {
  const [u, setU] = useState('oncall')
  const [p, setP] = useState('')
  const [err, setErr] = useState('')
  return (
    <div className="login card">
      <h1>tailhunt</h1>
      <p className="muted small">演示账号：oncall / oncall-demo（值班，能用控制台和教学模式）；guest / guest-demo（只能搜索）。</p>
      <form
        onSubmit={async (e) => {
          e.preventDefault()
          try {
            const r = await login(u, p)
            go(r.role === 'oncall' ? '/learn' : '/console')
          } catch (x: any) {
            setErr(x.message)
          }
        }}
      >
        <input name="username" value={u} onChange={(e) => setU(e.target.value)} placeholder="用户名" />
        <input name="password" type="password" value={p} onChange={(e) => setP(e.target.value)} placeholder="密码" />
        <button className="btn" type="submit">登录</button>
        {err && <p className="pill hot">{err}</p>}
      </form>
    </div>
  )
}
