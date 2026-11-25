import runbook from '../../../docs/runbook.md?raw'
import interview from '../../../docs/interview.md?raw'
import problems from '../../../docs/problems.md?raw'
import { Link, Markdown, Video, renderMarkdown } from '../components'

export function Runbook() {
  return <Markdown text={runbook} docPath="docs/runbook.md" />
}

function Tabs({ on }: { on: string }) {
  return (
    <div className="tabs">
      <Link to="/learn">排查流程</Link>
      <Link to="/learn/problems" className={on === 'problems' ? 'on' : ''} testid="tab-problems">日常问题</Link>
      <Link to="/learn/interview" className={on === 'interview' ? 'on' : ''} testid="tab-interview">面试题</Link>
    </div>
  )
}

// docs/interview.md: "## group" headings, "### question" headings, answer below.
export function Interview() {
  const groups = splitBy(interview, /^## (.+)$/m)
  return (
    <>
      <Tabs on="interview" />
      <h1>面试题</h1>
      <div dangerouslySetInnerHTML={{ __html: renderMarkdown(groups.intro, 'docs/interview.md') }} className="doc lede" />
      {groups.parts.map((g) => {
        const qs = splitBy(g.body, /^### (.+)$/m)
        return (
          <section key={g.title} data-testid="iv-group">
            <h2>{g.title}</h2>
            {qs.parts.map((q, i) => (
              <details key={i} className="q" data-testid="iv-q">
                <summary>{q.title}</summary>
                <div className="doc" data-testid="iv-answer" dangerouslySetInnerHTML={{ __html: renderMarkdown(q.body, 'docs/interview.md') }} />
              </details>
            ))}
          </section>
        )
      })}
    </>
  )
}

// docs/problems.md: each problem starts with <a id="qNN-..."></a> then "## title".
export function Problems() {
  const blocks = problems.split(/^<a id="(q\d\d-[a-z0-9-]+)"><\/a>\s*$/m)
  const intro = blocks[0]
  const items: { id: string; title: string; body: string }[] = []
  for (let i = 1; i < blocks.length; i += 2) {
    const text = blocks[i + 1]
    const m = text.match(/^\s*## (.+)$/m)
    items.push({ id: blocks[i], title: m ? m[1] : blocks[i], body: m ? text.slice(text.indexOf(m[0]) + m[0].length) : text })
  }
  return (
    <>
      <Tabs on="problems" />
      <h1>日常问题与解决</h1>
      <div className="doc lede" dangerouslySetInnerHTML={{ __html: renderMarkdown(intro.replace(/^# .+$/m, ''), 'docs/problems.md') }} />
      {items.map((p) => (
        <details key={p.id} className="q" data-testid="pb-item" id={p.id}>
          <summary>{p.title}</summary>
          <Video src={`/videos/problems/${p.id}.mp4`} testid="pb-video" />
          <div className="doc" data-testid="pb-body" dangerouslySetInnerHTML={{ __html: renderMarkdown(p.body, 'docs/problems.md') }} />
        </details>
      ))}
    </>
  )
}

function splitBy(text: string, re: RegExp) {
  const lines = text.split('\n')
  const parts: { title: string; body: string }[] = []
  const intro: string[] = []
  const g = new RegExp(re.source)
  for (const line of lines) {
    const m = line.match(g)
    if (m) parts.push({ title: m[1], body: '' })
    else if (parts.length) parts[parts.length - 1].body += line + '\n'
    else intro.push(line)
  }
  return { intro: intro.filter((l) => !/^# /.test(l)).join('\n'), parts }
}
