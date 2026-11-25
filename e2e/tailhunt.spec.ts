import { test, expect, type Page } from '@playwright/test'
import { readFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..')
const anchors = JSON.parse(readFileSync(join(ROOT, 'frontend/src/generated/anchors.json'), 'utf8'))
const LINK = /^https:\/\/github\.com\/[^/]+\/tailhunt\/blob\/([0-9a-f]{40}|HEAD)\/(.+)#L(\d+)-L(\d+)$/
const STAGES = ['alert', 'p99', 'trace', 'dbstats', 'culprit', 'fix', 'verify', 'history', 'runbook']

async function login(page: Page) {
  await page.goto('/login')
  await page.locator('input[name=password]').fill('oncall-demo')
  await page.getByRole('button', { name: '登录' }).click()
  await page.waitForURL('**/learn')
}

// A source link must have the right shape AND point at lines that exist in
// the file at that path, matching an anchor from anchors.json.
function checkSourceLink(href: string | null) {
  const m = href?.match(LINK)
  expect(m, `bad source link ${href}`).toBeTruthy()
  const [, , path, start, end] = m!
  const file = join(ROOT, path)
  expect(existsSync(file), `${path} does not exist`).toBe(true)
  const lines = readFileSync(file, 'utf8').split('\n')
  expect(+start).toBeGreaterThanOrEqual(1)
  expect(+end).toBeLessThanOrEqual(lines.length)
  expect(+start).toBeLessThanOrEqual(+end)
  expect(lines[+start - 1].trim(), `${path}:L${start} is blank`).not.toBe('')
  const a = Object.values(anchors).find((x: any) => x.path === path && x.start === +start && x.end === +end)
  expect(a, `${path}#L${start}-L${end} is not an anchor`).toBeTruthy()
}

const num = (s: string) => parseFloat(s.replace(/[^\d.]/g, '')) * (/\bs$/.test(s.trim()) && !/ms$/.test(s.trim()) ? 1000 : 1)

test('learn mode: one real investigation, stage by stage', async ({ page }) => {
  test.setTimeout(6 * 60_000)
  await login(page)
  await page.getByTestId('learn-start').click()
  for (const [i, id] of STAGES.entries()) {
    const stage = page.getByTestId('stage-' + id)
    await expect(stage).toHaveAttribute('data-status', 'done', { timeout: 120_000 })
    // stages light in order: nothing after this one is done yet
    for (const later of STAGES.slice(i + 1)) await expect(page.getByTestId('stage-' + later)).toHaveAttribute('data-status', 'pending')
    await expect(page.getByTestId('stage-detail')).toHaveAttribute('data-stage', id)
    const ev = page.getByTestId('evidence')
    switch (id) {
      case 'alert':
        await expect(ev.getByTestId('alert-name')).toHaveText('SearchP99High')
        await expect(ev.getByTestId('alert-state')).toHaveText('firing')
        break
      case 'p99':
        await expect(ev.getByTestId('p99-chart')).toBeVisible()
        expect(num(await ev.getByTestId('p99-peak').innerText())).toBeGreaterThan(300)
        expect(num(await ev.getByTestId('p99-calm').innerText())).toBeLessThan(100)
        break
      case 'trace': {
        await expect(ev.getByTestId('trace-id')).toHaveText(/^[0-9a-f]{32}$/)
        await expect(ev.getByTestId('span-acquire')).toBeVisible()
        const first = ev.getByTestId('waterfall').locator('.r').first()
        await expect(first).toContainText('gateway')
        expect(num(await ev.getByTestId('trace-total').innerText())).toBeGreaterThanOrEqual(num(await ev.getByTestId('trace-acquire').innerText()))
        await expect(ev.getByTestId('jaeger-link')).toHaveAttribute('href', /\/trace\/[0-9a-f]{32}$/)
        break
      }
      case 'dbstats': {
        const [inUse, max] = (await ev.getByTestId('db-inuse').innerText()).split('/').map((x) => +x.trim())
        expect(inUse).toBe(max)
        expect(num(await ev.getByTestId('db-waits').innerText())).toBeGreaterThan(0)
        break
      }
      case 'culprit':
        expect(+(await ev.getByTestId('idle-count').innerText())).toBeGreaterThan(0)
        await expect(ev.getByTestId('pg-row').first()).toContainText('FOR UPDATE')
        await expect(ev.getByTestId('cfg-txscope')).toHaveText('false')
        break
      case 'fix':
        await expect(ev.getByTestId('fix-quote_outside_tx')).toContainText('true')
        break
      case 'verify': {
        const before = num(await ev.getByTestId('p99-before').innerText())
        const after = num(await ev.getByTestId('p99-after').innerText())
        expect(after).toBeLessThan(100)
        expect(after).toBeLessThan(before)
        await expect(ev.getByTestId('verify-waits')).toHaveText('0')
        break
      }
      case 'history':
        await expect(ev.getByTestId('cause-db_pool_exhaustion')).toContainText('#1')
        break
      case 'runbook':
        await expect(ev.getByTestId('runbook-steps').locator('li')).toHaveCount(9)
        break
    }
    // raw JSON is the real response, and every source link points at real lines
    await page.getByTestId('raw-json').locator('summary').click()
    await expect(page.getByTestId('raw-json').locator('pre')).toContainText('{')
    for (const a of await page.getByTestId('src-list').locator('a').all()) checkSourceLink(await a.getAttribute('href'))
    checkSourceLink(await page.getByTestId('src-' + id).getAttribute('href'))
    if (id !== 'runbook') await page.getByTestId('learn-next').click()
  }
  await expect(page.getByTestId('learn-state')).toContainText('已完成')
})

test('learn mode: a stage video slot says the video is not generated yet', async ({ page }) => {
  await login(page)
  await page.getByTestId('video-btn-trace').click()
  await expect(page.getByTestId('stage-video')).toHaveAttribute('data-video', /missing|ready/)
  if ((await page.getByTestId('stage-video').getAttribute('data-video')) === 'missing') await expect(page.getByTestId('stage-video')).toContainText('视频待生成')
})

test('interview tab: open a question and read its answer', async ({ page }) => {
  await login(page)
  await page.getByTestId('tab-interview').click()
  await page.waitForURL('**/learn/interview')
  const qs = page.getByTestId('iv-q')
  expect(await qs.count()).toBeGreaterThanOrEqual(25)
  await qs.first().locator('summary').click()
  await expect(qs.first().getByTestId('iv-answer')).toContainText('一句话答案')
})

test('problems tab: expand a problem and see symptom, fix, proof and the video slot', async ({ page }) => {
  await login(page)
  await page.getByTestId('tab-problems').click()
  await page.waitForURL('**/learn/problems')
  const items = page.getByTestId('pb-item')
  expect(await items.count()).toBeGreaterThanOrEqual(6)
  await items.first().locator('summary').click()
  const body = items.first().getByTestId('pb-body')
  for (const word of ['现象', '在哪里', '怎么定位', '怎么解决', '怎么证明', '面试怎么讲']) await expect(body).toContainText(word)
  checkSourceLink(await body.locator('a.src-link').first().getAttribute('href'))
  await expect(items.first().getByTestId('pb-video')).toHaveAttribute('data-video', /missing|ready/)
})

test('analysis page: synthetic badge and the planted top cause', async ({ page }) => {
  await login(page)
  await page.getByTestId('nav-analysis').click()
  await expect(page.getByTestId('synthetic-badge')).toHaveText('合成数据')
  await expect(page.getByTestId('top-cause')).toHaveText('db_pool_exhaustion')
  await expect(page.getByTestId('rule-DiskUsageHigh')).toContainText('94%')
})

test('console: live chart, pool and switches answer', async ({ page }) => {
  await login(page)
  await page.getByTestId('nav-console').click()
  await expect(page.getByTestId('pool-inuse')).toHaveText(/^\d+\/6$/)
  await page.getByTestId('mode-fixed').click()
  await expect(page.getByTestId('mode-state')).toHaveText('fixed')
})

test('runbook page renders the steps', async ({ page }) => {
  await login(page)
  await page.getByTestId('nav-runbook').click()
  await expect(page.locator('.doc h2')).toHaveCount(10)
  await expect(page.locator('.doc img').first()).toBeVisible()
})
