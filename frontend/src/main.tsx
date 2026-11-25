import { StrictMode, useEffect, useState } from 'react'
import { createRoot } from 'react-dom/client'
import './style.css'
import { api, token } from './api'
import { Shell, go, usePath } from './components'
import { Login } from './pages/Login'
import { Console } from './pages/Console'
import { Analysis } from './pages/Analysis'
import { Learn } from './pages/Learn'
import { Interview, Problems, Runbook } from './pages/Docs'

function App() {
  const path = usePath()
  const [me, setMe] = useState<{ sub: string; role: string } | null>(null)
  useEffect(() => {
    if (path === '/login') return
    if (!token()) return go('/login')
    api('/me').then(setMe).catch(() => go('/login'))
  }, [path])
  if (path === '/login') return <Login />
  if (!me) return null
  if (path === '/') {
    go('/learn')
    return null
  }
  const page =
    path === '/console' ? <Console /> :
    path === '/analysis' ? <Analysis /> :
    path === '/runbook' ? <Runbook /> :
    path === '/learn/interview' ? <Interview /> :
    path === '/learn/problems' ? <Problems /> :
    path === '/learn' ? <Learn /> : <p>没有这个页面。</p>
  return <Shell path={path} user={`${me.sub} · ${me.role}`}>{page}</Shell>
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
