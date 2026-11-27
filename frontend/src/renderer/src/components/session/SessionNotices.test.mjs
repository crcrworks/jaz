import { expect, test } from 'bun:test'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { SessionNotices } from '@/components/session/SessionNotices'

test('a session says which effort it runs instead of the one it was given', () => {
  const html = renderToStaticMarkup(createElement(SessionNotices, {
    notices: ['opencode did not offer reasoning effort medium for model deepseek/deepseek-v4.1-flash, so this thread runs on the model\'s own default. Values it offers: high, low, medium.'],
  }))
  expect(html).toContain('role="status"')
  expect(html).toContain('reasoning effort medium')
  expect(html).toContain('own default')
})

test('a session with nothing to report renders nothing', () => {
  expect(renderToStaticMarkup(createElement(SessionNotices, {}))).toBe('')
  expect(renderToStaticMarkup(createElement(SessionNotices, { notices: [] }))).toBe('')
  expect(renderToStaticMarkup(createElement(SessionNotices, { notices: ['  '] }))).toBe('')
})
