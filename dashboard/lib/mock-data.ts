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

// Mock data layer simulating MSW responses

export interface Template {
  id: string
  name: string
  aliases: number
  buildConfig: string
  cpuCount: number
  memoryMB: number
  createdAt: string
  updatedAt: string
  visibility: "PUBLIC" | "PRIVATE"
  envdVersion: string
}

export interface Sandbox {
  id: string
  templateId: string
  templateName: string
  cpuCount: number
  memoryMB: number
  diskMB: number
  metadata: Record<string, string>
  startedAt: string
}

export interface Build {
  id: string
  templateId: string
  templateName: string
  status: "READY" | "BUILDING" | "ERROR"
  cpuCount: number
  memoryMB: number
  createdAt: string
  finishedAt: string | null
}

export interface ApiKey {
  id: string
  name: string
  maskedKey: string
  createdAt: string
  lastUsedAt: string | null
}

export interface TeamMember {
  id: string
  email: string
  role: "OWNER" | "ADMIN" | "MEMBER"
  joinedAt: string
}

export interface Webhook {
  id: string
  url: string
  events: string[]
  active: boolean
  createdAt: string
}

// --- Mock Templates ---
export const mockTemplates: Template[] = [
  {
    id: "k0wmnzir0zuzye6dnd1w",
    name: "desktop",
    aliases: 1,
    buildConfig: "BY",
    cpuCount: 8,
    memoryMB: 8192,
    createdAt: "2024-05-27T01:50:00Z",
    updatedAt: "2026-02-17T09:49:00Z",
    visibility: "PUBLIC",
    envdVersion: "0.5.2",
  },
  {
    id: "n1hz8v1wyupq845jsdg9",
    name: "code-interpreter-v1",
    aliases: 1,
    buildConfig: "BY",
    cpuCount: 2,
    memoryMB: 2048,
    createdAt: "2024-10-16T02:45:00Z",
    updatedAt: "2026-02-13T00:49:00Z",
    visibility: "PUBLIC",
    envdVersion: "0.5.2",
  },
  {
    id: "rki5dems9wqfm4r03t7g",
    name: "base",
    aliases: 3,
    buildConfig: "BY",
    cpuCount: 2,
    memoryMB: 512,
    createdAt: "2023-11-01T16:21:00Z",
    updatedAt: "2026-02-11T11:01:00Z",
    visibility: "PUBLIC",
    envdVersion: "0.5.2",
  },
  {
    id: "x9ab3kdf7wmn2pos1h4q",
    name: "data-science-env",
    aliases: 0,
    buildConfig: "BY",
    cpuCount: 4,
    memoryMB: 4096,
    createdAt: "2025-01-10T08:30:00Z",
    updatedAt: "2026-01-28T14:22:00Z",
    visibility: "PRIVATE",
    envdVersion: "0.5.2",
  },
  {
    id: "m3pq8rvt6wnz5bcd2e0j",
    name: "web-scraper",
    aliases: 2,
    buildConfig: "BY",
    cpuCount: 2,
    memoryMB: 1024,
    createdAt: "2025-06-15T10:00:00Z",
    updatedAt: "2026-02-10T07:15:00Z",
    visibility: "PUBLIC",
    envdVersion: "0.5.2",
  },
]

// --- Mock Sandboxes ---
export const mockSandboxes: Sandbox[] = [
  {
    id: "sbx-a1b2c3d4e5f6",
    templateId: "k0wmnzir0zuzye6dnd1w",
    templateName: "desktop",
    cpuCount: 8,
    memoryMB: 8192,
    diskMB: 5120,
    metadata: { user: "demo", purpose: "testing" },
    startedAt: "2026-02-25T08:30:00Z",
  },
  {
    id: "sbx-g7h8i9j0k1l2",
    templateId: "n1hz8v1wyupq845jsdg9",
    templateName: "code-interpreter-v1",
    cpuCount: 2,
    memoryMB: 2048,
    diskMB: 2048,
    metadata: { session: "abc123" },
    startedAt: "2026-02-25T09:15:00Z",
  },
  {
    id: "sbx-m3n4o5p6q7r8",
    templateId: "rki5dems9wqfm4r03t7g",
    templateName: "base",
    cpuCount: 2,
    memoryMB: 512,
    diskMB: 512,
    metadata: {},
    startedAt: "2026-02-25T10:00:00Z",
  },
]

// --- Mock Builds ---
export const mockBuilds: Build[] = [
  {
    id: "bld-001",
    templateId: "k0wmnzir0zuzye6dnd1w",
    templateName: "desktop",
    status: "READY",
    cpuCount: 8,
    memoryMB: 8192,
    createdAt: "2026-02-17T09:40:00Z",
    finishedAt: "2026-02-17T09:49:00Z",
  },
  {
    id: "bld-002",
    templateId: "n1hz8v1wyupq845jsdg9",
    templateName: "code-interpreter-v1",
    status: "READY",
    cpuCount: 2,
    memoryMB: 2048,
    createdAt: "2026-02-13T00:40:00Z",
    finishedAt: "2026-02-13T00:49:00Z",
  },
  {
    id: "bld-003",
    templateId: "m3pq8rvt6wnz5bcd2e0j",
    templateName: "web-scraper",
    status: "BUILDING",
    cpuCount: 2,
    memoryMB: 1024,
    createdAt: "2026-02-25T11:00:00Z",
    finishedAt: null,
  },
  {
    id: "bld-004",
    templateId: "x9ab3kdf7wmn2pos1h4q",
    templateName: "data-science-env",
    status: "ERROR",
    cpuCount: 4,
    memoryMB: 4096,
    createdAt: "2026-02-20T06:00:00Z",
    finishedAt: "2026-02-20T06:05:00Z",
  },
]

// --- Mock API Keys ---
export const mockApiKeys: ApiKey[] = [
  {
    id: "key-001",
    name: "Production Key",
    maskedKey: "e2b_****************************a3f1",
    createdAt: "2025-06-01T00:00:00Z",
    lastUsedAt: "2026-02-25T10:00:00Z",
  },
  {
    id: "key-002",
    name: "Development Key",
    maskedKey: "e2b_****************************b7c2",
    createdAt: "2025-09-15T00:00:00Z",
    lastUsedAt: "2026-02-24T18:30:00Z",
  },
]

// --- Mock Team Members ---
export const mockMembers: TeamMember[] = [
  {
    id: "usr-001",
    email: "admin@example.com",
    role: "OWNER",
    joinedAt: "2023-01-01T00:00:00Z",
  },
  {
    id: "usr-002",
    email: "dev@example.com",
    role: "ADMIN",
    joinedAt: "2024-03-15T00:00:00Z",
  },
  {
    id: "usr-003",
    email: "viewer@example.com",
    role: "MEMBER",
    joinedAt: "2025-07-20T00:00:00Z",
  },
]

// --- Mock Webhooks ---
export const mockWebhooks: Webhook[] = [
  {
    id: "wh-001",
    url: "https://api.example.com/hooks/sandbox-events",
    events: ["sandbox.started", "sandbox.stopped"],
    active: true,
    createdAt: "2025-08-01T00:00:00Z",
  },
]

// --- Mock API functions (simulating MSW handlers) ---
const delay = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

export async function fetchTemplates(): Promise<Template[]> {
  await delay(300)
  return mockTemplates
}

export async function fetchSandboxes(): Promise<Sandbox[]> {
  await delay(200)
  return mockSandboxes
}

export async function fetchBuilds(): Promise<Build[]> {
  await delay(250)
  return mockBuilds
}

export async function fetchApiKeys(): Promise<ApiKey[]> {
  await delay(200)
  return mockApiKeys
}

export async function fetchMembers(): Promise<TeamMember[]> {
  await delay(200)
  return mockMembers
}

export async function fetchWebhooks(): Promise<Webhook[]> {
  await delay(200)
  return mockWebhooks
}
