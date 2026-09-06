// Large map of unambiguous language aliases → canonical file extension (without dot).
// Only non-1:1 mappings are included: e.g. `js -> js` is omitted — Case 3 returns raw lang as ext.
// Keys are lowercased. Values are conventional extensions.

export const languageExtensions: Record<string, string> = {
  ada: 'adb',
  assembly: 'asm',
  bash: 'sh',
  batch: 'bat',
  'c#': 'cs',
  'c++': 'cpp',
  coffeescript: 'coffee',
  cplusplus: 'cpp',
  clojure: 'clj',
  clojurescript: 'cljs',
  cobol: 'cbl',
  crystal: 'cr',
  csharp: 'cs',
  cypher: 'cyp',
  delphi: 'pas',
  elisp: 'el',
  elixir: 'ex',
  erlang: 'erl',
  exs: 'ex',
  'f#': 'fs',
  f03: 'f90',
  f95: 'f90',
  fortran: 'f90',
  fsharp: 'fs',
  golang: 'go',
  gql: 'graphql',
  handlebars: 'hbs',
  haskell: 'hs',
  jade: 'pug',
  javascript: 'js',
  julia: 'jl',
  kotlin: 'kt',
  latex: 'tex',
  livescript: 'ls',
  make: 'makefile',
  markdown: 'md',
  matlab: 'm',
  nginx: 'conf',
  objc: 'm',
  'objective-c': 'm',
  objectivec: 'm',
  ocaml: 'ml',
  octave: 'm',
  pascal: 'pas',
  perl: 'pl',
  powershell: 'ps1',
  prolog: 'pro',
  psm1: 'ps1',
  purescript: 'purs',
  pwsh: 'ps1',
  py3: 'py',
  python: 'py',
  python3: 'py',
  racket: 'rkt',
  reason: 're',
  reasonml: 're',
  ruby: 'rb',
  rust: 'rs',
  s: 'asm',
  scheme: 'scm',
  shell: 'sh',
  solidity: 'sol',
  sparql: 'rq',
  stylus: 'styl',
  systemverilog: 'sv',
  terraform: 'tf',
  typescript: 'ts',
  verilog: 'v',
  vyper: 'vy',
  yml: 'yaml',
}

const TXT_ALIASES = new Set(['code', 'plaintext', 'text', 'plain', 'txt'])

function normalize(raw?: string): string {
  if (!raw) return ''
  // first token of info string, trim, lower, strip leading dots
  const first = raw.trim().toLowerCase().split(/\s+/)[0] ?? ''
  return first.replace(/^\.+/, '')
}

function sanitize(ext: string): string {
  // allow a-z0-9 . _ - ; replace anything else with '-'
  const cleaned = ext.replace(/[^a-z0-9._-]/g, '-').replace(/^-+/, '').slice(0, 20)
  return cleaned || 'txt'
}

export function resolveExtension(raw?: string): string {
  const n = normalize(raw)
  if (!n || TXT_ALIASES.has(n)) return 'txt' // Case 1
  if (n in languageExtensions) return languageExtensions[n] // Case 2
  return sanitize(n) // Case 3: use language as extension directly
}
