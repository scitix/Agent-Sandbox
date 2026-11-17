import type { AgentInstanceTypeItem } from "@/lib/api/client"

const NVIDIA_RESOURCE = "nvidia.com/gpu"

// The catalog identifies the count resource explicitly. Do not infer a GPU from
// arbitrary extended resources: RDMA, NICs and hugepages are independent devices.
export function instanceGpuResourceName(it: AgentInstanceTypeItem | null | undefined): string {
  return it?.extensions?.["gpu-resource-name"]?.trim() || NVIDIA_RESOURCE
}

export function instanceGpuResources(
  it: AgentInstanceTypeItem | null | undefined,
  multiplier: number | string | undefined,
): Record<string, string> {
  const key = instanceGpuResourceName(it)
  const count = Number(it?.baseResources?.requests?.[key] ?? it?.baseResources?.limits?.[key])
  const raw = Number(multiplier)
  const scale = Number.isSafeInteger(raw) && raw >= 1 ? raw : 1
  const total = count * scale
  return Number.isSafeInteger(count) && count > 0 && Number.isSafeInteger(total)
    ? { [key]: String(total) }
    : {}
}

export function instanceGpuPreview(
  it: AgentInstanceTypeItem | null | undefined,
  multiplier: number | string | undefined,
): string | null {
  const resource = instanceGpuResourceName(it)
  const count = instanceGpuResources(it, multiplier)[resource]
  if (!count) return null
  const model = it?.extensions?.["gpu-model"] || it?.extensions?.["gpu-type"]
  const label = resource.toLowerCase().includes("ascend")
    ? "NPU"
    : resource === NVIDIA_RESOURCE || resource === "amd.com/gpu"
      ? "GPU"
      : resource
  return `${count} ${label}${model ? ` (${model})` : ""}`
}

export type PoolResources = {
  requests?: Record<string, string>
  limits?: Record<string, string>
}

// Editing pool bounds cannot change its resource shape. Keep every live
// dimension, including vendor devices, RDMA and ephemeral storage.
export function preservePoolResources(
  body: Record<string, unknown>,
  resources: PoolResources | undefined,
): void {
  if (resources !== undefined) body.inlineResources = resources
}
