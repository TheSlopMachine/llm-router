// Markdown rendering for chat messages.
//
// The previous version passed a `highlight` callback to marked.setOptions.
// That option was removed in marked v5; on marked 18 it is silently ignored,
// so the callback never fired -- while `import hljs from 'highlight.js'`
// still pulled in all ~190 language grammars. The result was roughly two
// thirds of the JS bundle spent on syntax highlighting that did not happen.
//
// Highlighting now runs through marked-highlight against a core build with an
// explicit language list, and the whole module is loaded on demand (only Chat
// needs it), so it no longer weighs on first paint.
import { Marked } from 'marked'
import { markedHighlight } from 'marked-highlight'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'

// Registered explicitly: each entry is a few KB, the full package is ~900.
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import rust from 'highlight.js/lib/languages/rust'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import yaml from 'highlight.js/lib/languages/yaml'
import xml from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import sql from 'highlight.js/lib/languages/sql'
import markdown from 'highlight.js/lib/languages/markdown'
import diff from 'highlight.js/lib/languages/diff'
import lua from 'highlight.js/lib/languages/lua'
import 'highlight.js/styles/github.css'

for (const [name, lang] of [
  ['javascript', javascript],
  ['typescript', typescript],
  ['python', python],
  ['go', go],
  ['rust', rust],
  ['bash', bash],
  ['json', json],
  ['yaml', yaml],
  ['xml', xml],
  ['css', css],
  ['sql', sql],
  ['markdown', markdown],
  ['diff', diff],
  ['lua', lua],
] as const) {
  hljs.registerLanguage(name, lang)
}

const marked = new Marked(
  markedHighlight({
    emptyLangClass: 'hljs',
    langPrefix: 'hljs language-',
    highlight(code: string, lang: string) {
      const language = lang && hljs.getLanguage(lang) ? lang : 'plaintext'
      if (language === 'plaintext') return code
      try {
        return hljs.highlight(code, { language }).value
      } catch {
        return code
      }
    },
  }),
  { gfm: true, breaks: true }
)

export interface ParsedArtifact {
  title: string
  code: string
  language: string
  collapsed: boolean
}

export function parseMarkdownWithArtifacts(content: string): {
  html: string
  artifacts: ParsedArtifact[]
} {
  const artifacts: ParsedArtifact[] = []
  // [^\s`]+ captures c++, c#, objective-c, .js etc; trailing [ \t]* allows "```js linenos"
  const re = /```([^\s`]+)?[ \t]*\n([\s\S]*?)```/g
  let m: RegExpExecArray | null
  while ((m = re.exec(content)) !== null) {
    const lang = m[1]?.trim() ?? ''
    const code = m[2].trim()
    artifacts.push({ title: lang || 'code', code, language: lang, collapsed: false })
  }
  const clean = content.replace(re, '').trim()
  let html = ''
  if (clean) {
    html = DOMPurify.sanitize(marked.parse(clean) as string)
  } else if (artifacts.length === 0) {
    html = DOMPurify.sanitize(marked.parse(content) as string)
  }
  return { html, artifacts }
}

export function renderMarkdown(src: string): string {
  return DOMPurify.sanitize(marked.parse(src) as string)
}
