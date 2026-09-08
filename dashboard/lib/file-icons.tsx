/**
 * Copyright 2026 ScitiX
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import {
  File,
  FileCode2,
  FileImage,
  FileJson,
  FileText,
  Folder,
  type LucideIcon,
} from 'lucide-react'

function ext(name: string): string {
  const m = /\.([a-z0-9]+)$/i.exec(name)
  return m ? m[1].toLowerCase() : ''
}

const CODE_EXTS = new Set([
  'py',
  'ts',
  'tsx',
  'js',
  'jsx',
  'go',
  'sh',
  'bash',
  'rs',
  'java',
  'c',
  'cc',
  'cpp',
  'h',
  'hpp',
  'rb',
  'php',
  'sql',
  'css',
  'scss',
  'html',
])
const CONFIG_EXTS = new Set([
  'yaml',
  'yml',
  'toml',
  'ini',
  'cfg',
  'conf',
  'env',
  'xml',
])
const DOC_EXTS = new Set(['md', 'markdown', 'txt', 'log', 'csv', 'rst'])
const IMAGE_EXTS = new Set([
  'png',
  'jpg',
  'jpeg',
  'gif',
  'webp',
  'svg',
  'bmp',
  'ico',
  'avif',
])

// Map a directory entry to a lucide icon. Directories always get a folder.
export function iconForEntry(entry: {
  name: string
  type: 'dir' | 'file'
}): LucideIcon {
  if (entry.type === 'dir') return Folder
  const e = ext(entry.name)
  if (IMAGE_EXTS.has(e)) return FileImage
  if (e === 'json') return FileJson
  if (CODE_EXTS.has(e) || CONFIG_EXTS.has(e)) return FileCode2
  if (DOC_EXTS.has(e)) return FileText
  return File
}

const BINARY_EXTS = new Set([
  'zip',
  'tar',
  'gz',
  'tgz',
  'bz2',
  'xz',
  '7z',
  'rar',
  'pdf',
  'exe',
  'bin',
  'so',
  'dylib',
  'o',
  'a',
  'class',
  'jar',
  'wasm',
  'mp4',
  'mp3',
  'wav',
  'mov',
  'woff',
  'woff2',
  'ttf',
  'otf',
  'parquet',
  'pkl',
  'npy',
  'pt',
  'pth',
  'onnx',
  'h5',
  'db',
  'sqlite',
])

export function isImageName(name: string): boolean {
  return IMAGE_EXTS.has(ext(name))
}

// Whether a file should be fetched as text for inline preview. Images and known
// binary formats are excluded (they get a download affordance instead); an
// extensionless file is assumed textual (agent logs/reports usually are).
export function isPreviewableText(name: string): boolean {
  const e = ext(name)
  if (!e) return true
  return !IMAGE_EXTS.has(e) && !BINARY_EXTS.has(e)
}

export function isMarkdownName(name: string): boolean {
  const e = ext(name)
  return e === 'md' || e === 'markdown'
}

// Best-effort MIME for building an image data: URI in the preview.
export function imageMimeForName(name: string): string {
  const e = ext(name)
  if (e === 'svg') return 'image/svg+xml'
  if (e === 'jpg' || e === 'jpeg') return 'image/jpeg'
  if (e === 'ico') return 'image/x-icon'
  return `image/${e || 'png'}`
}

// Syntax-highlight language hint for the code viewer. CodeBlock loads the
// matching shiki grammar on demand and falls through to plain monospace for
// anything it does not carry (`text`).
export function languageForName(name: string): string {
  const e = ext(name)
  const map: Record<string, string> = {
    py: 'python',
    md: 'markdown',
    markdown: 'markdown',
    json: 'json',
    yaml: 'yaml',
    yml: 'yaml',
    sh: 'bash',
    bash: 'bash',
    ts: 'typescript',
    tsx: 'typescript',
    js: 'javascript',
    jsx: 'javascript',
    go: 'go',
    toml: 'toml',
    xml: 'xml',
    css: 'css',
    html: 'html',
    sql: 'sql',
    rs: 'rust',
    java: 'java',
    c: 'c',
    cpp: 'cpp',
    rb: 'ruby',
  }
  if (/^dockerfile$/i.test(name)) return 'docker'
  return map[e] || 'text'
}
