import { describe, expect, it } from "vitest"
import type { AgentInstanceTypeItem } from "@/lib/api/client"
import {
  instanceGpuResources,
  instanceGpuPreview,
  preservePoolResources,
} from "@/lib/utils/instance-gpu"

function instance(resources: Record<string, string>, extensions: Record<string, string> = {}) {
  return {
    name: "test-device",
    baseResources: { requests: { cpu: "8", memory: "16Gi", ...resources } },
    extensions,
  } as AgentInstanceTypeItem
}

describe("catalog accelerator display and resource preservation", () => {
  it("defaults to NVIDIA and supports the legacy model field", () => {
    const gpu = instance({ "nvidia.com/gpu": "1" }, { "gpu-type": "H100" })
    expect(instanceGpuResources(gpu, 2)).toEqual({ "nvidia.com/gpu": "2" })
    expect(instanceGpuPreview(gpu, 2)).toBe("2 GPU (H100)")
  })

  it("uses the declared Ascend resource and model without inventing NVIDIA requests", () => {
    const npu = instance(
      { "huawei.com/Ascend910": "2", "rdma/hca_shared_devices_a": "1" },
      { "gpu-resource-name": "huawei.com/Ascend910", "gpu-model": "Ascend910B" },
    )
    expect(instanceGpuResources(npu, 4)).toEqual({ "huawei.com/Ascend910": "8" })
    expect(instanceGpuPreview(npu, 4)).toBe("8 NPU (Ascend910B)")
  })

  it("supports AMD and limits-only catalog resources", () => {
    const gpu = {
      name: "amd",
      baseResources: { requests: { cpu: "8" }, limits: { "amd.com/gpu": "1" } },
      extensions: { "gpu-resource-name": "amd.com/gpu", "gpu-model": "MI300X" },
    } as AgentInstanceTypeItem
    expect(instanceGpuResources(gpu, "2")).toEqual({ "amd.com/gpu": "2" })
    expect(instanceGpuPreview(gpu, 2)).toBe("2 GPU (MI300X)")
  })

  it("does not classify CPU or RDMA-only instances as GPUs", () => {
    for (const gpu of [null, instance({}), instance({ "rdma/hca_shared_devices_a": "4" })]) {
      expect(instanceGpuResources(gpu, 2)).toEqual({})
      expect(instanceGpuPreview(gpu, 2)).toBeNull()
    }
  })

  it.each(["0", "-1", "0.5", "NaN", "Infinity"])("rejects invalid GPU count %s", (count) => {
    expect(instanceGpuResources(instance({ "nvidia.com/gpu": count }), 2)).toEqual({})
  })

  it("keeps all immutable request/limit dimensions on a bounds-only edit", () => {
    const resources = {
      requests: {
        cpu: "2",
        memory: "8Gi",
        "huawei.com/Ascend910": "2",
        "rdma/hca_shared_devices_a": "1",
        "ephemeral-storage": "32Gi",
      },
      limits: {
        cpu: "2",
        memory: "8Gi",
        "huawei.com/Ascend910": "2",
        "rdma/hca_shared_devices_a": "1",
        "ephemeral-storage": "32Gi",
      },
    }
    const body: Record<string, unknown> = {
      replicas: 3,
      inlineResources: { requests: { cpu: "2", memory: "8Gi" } },
    }
    preservePoolResources(body, resources)
    expect(body.inlineResources).toEqual(resources)
    expect(body.replicas).toBe(3)
  })
})
