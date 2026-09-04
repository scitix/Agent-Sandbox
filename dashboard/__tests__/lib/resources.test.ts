import { describe, expect, it } from "vitest"

import {
  cpuQuantity,
  memoryQuantity,
  splitCpu,
  splitMemory,
  toMiB,
  toMilliCores,
} from "@/lib/resources"

describe("toMilliCores", () => {
  it("scales whole cores to milli", () => {
    expect(toMilliCores(2, "core")).toBe(2000)
    expect(toMilliCores("1", "core")).toBe(1000)
  })

  it("passes milli through untouched", () => {
    expect(toMilliCores(20, "milli")).toBe(20)
  })

  it("treats blank and non-numeric input as absent", () => {
    expect(toMilliCores(undefined, "core")).toBeUndefined()
    expect(toMilliCores("", "core")).toBeUndefined()
    expect(toMilliCores("abc", "core")).toBeUndefined()
  })
})

describe("toMiB", () => {
  it("scales GiB to MiB", () => {
    expect(toMiB(8, "Gi")).toBe(8192)
  })

  it("passes MiB through untouched", () => {
    expect(toMiB(128, "Mi")).toBe(128)
  })

  it("treats blank input as absent", () => {
    expect(toMiB(undefined, "Gi")).toBeUndefined()
    expect(toMiB("", "Mi")).toBeUndefined()
  })
})

describe("cpuQuantity / memoryQuantity", () => {
  it("renders canonical Kubernetes quantities", () => {
    expect(cpuQuantity(2, "core")).toBe("2")
    expect(cpuQuantity(20, "milli")).toBe("20m")
    expect(memoryQuantity(8, "Gi")).toBe("8Gi")
    expect(memoryQuantity(128, "Mi")).toBe("128Mi")
  })
})

describe("splitCpu", () => {
  it("keeps whole cores in core units", () => {
    expect(splitCpu(1)).toEqual({ value: 1, unit: "core" })
    expect(splitCpu(4)).toEqual({ value: 4, unit: "core" })
  })

  it("falls back to milli for sub-core amounts", () => {
    expect(splitCpu(0.02)).toEqual({ value: 20, unit: "milli" })
    expect(splitCpu(0.5)).toEqual({ value: 500, unit: "milli" })
    expect(splitCpu(1.5)).toEqual({ value: 1500, unit: "milli" })
  })
})

describe("splitMemory", () => {
  it("keeps whole GiB in GiB units", () => {
    expect(splitMemory(1024)).toEqual({ value: 1, unit: "Gi" })
    expect(splitMemory(16384)).toEqual({ value: 16, unit: "Gi" })
  })

  it("falls back to MiB for non-whole GiB amounts", () => {
    expect(splitMemory(128)).toEqual({ value: 128, unit: "Mi" })
    expect(splitMemory(1536)).toEqual({ value: 1536, unit: "Mi" })
  })
})

// A Pool sized in milli-cores / MiB must reopen showing exactly what it was
// created with — the round trip is what keeps the Edit form from silently
// resizing a Pod when the user saves an unrelated field.
describe("form round trip", () => {
  const cases: Array<[string, string]> = [
    ["20m", "128Mi"],
    ["500m", "2Gi"],
    ["1", "16Gi"],
    ["2", "1536Mi"],
  ]

  it.each(cases)("preserves %s / %s", (cpu, memory) => {
    const cores = cpu.endsWith("m") ? parseInt(cpu, 10) / 1000 : Number(cpu)
    const mib = memory.endsWith("Gi") ? parseInt(memory, 10) * 1024 : parseInt(memory, 10)

    const cpuAmount = splitCpu(cores)
    const memAmount = splitMemory(mib)

    expect(cpuQuantity(cpuAmount.value, cpuAmount.unit)).toBe(cpu)
    expect(memoryQuantity(memAmount.value, memAmount.unit)).toBe(memory)
    expect(toMilliCores(cpuAmount.value, cpuAmount.unit)).toBe(Math.round(cores * 1000))
    expect(toMiB(memAmount.value, memAmount.unit)).toBe(mib)
  })
})
