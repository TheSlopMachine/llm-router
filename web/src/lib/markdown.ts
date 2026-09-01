import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js'
import 'highlight.js/styles/github.css'

marked.setOptions({
  gfm: true,
  breaks: true,
  highlight: (code: string, lang?: string) => {
    if (lang && hljs.getLanguage(lang)) {
      try {
        return hljs.highlight(code, { language: lang }).value
      } catch {
        return code
      }
    }
    try {
      return hljs.highlightAuto(code).value
    } catch {
      return code
    }
  }
})

export interface ParsedArtifact {
  title: string
  code: string
  language: string
  collapsed: boolean
}

export function parseMarkdownWithArtifacts(content: string): { html: string; artifacts: ParsedArtifact[] } {
  const artifacts: ParsedArtifact[] = []
  const re = /```(\w+)?\n([\s\S]*?)```/g
  let m: RegExpExecArray | null
  while ((m = re.exec(content)) !== null) {
    const lang = m[1]?.trim() || 'code'
    const code = m[2].trim()
    artifacts.push({ title: lang, code, language: lang, collapsed: false })
  }
  const clean = content.replace(re, '').trim()
  let html = ''
  if (clean) {
    const raw = marked.parse(clean) as string
    html = DOMPurify.sanitize(raw)
  } else if (artifacts.length === 0) {
    const raw = marked.parse(content) as string
    html = DOMPurify.sanitize(raw)
  }
  return { html, artifacts }
}

export function renderMarkdown(src: string): string {
  const raw = marked.parse(src) as string
  return DOMPurify.sanitize(raw)
}
