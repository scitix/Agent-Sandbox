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

import { describe, it, expect } from "vitest"

import { clusterQueryKey } from "@/lib/api/cluster-query-key"

describe("clusterQueryKey", () => {
  it("separates the same operation across clusters", () => {
    const init = { params: { path: { name: "slimedev" } } }
    expect(clusterQueryKey("get", "/envs/{name}", init, "foo")).not.toEqual(
      clusterQueryKey("get", "/envs/{name}", init, "bar"),
    )
  })

  it("keeps init at index 2 so the library's queryFn can spread it", () => {
    const init = { params: { path: { name: "slimedev" } } }
    const key = clusterQueryKey("get", "/envs/{name}", init, "foo")
    expect(key[2]).toBe(init)
    expect(key).toEqual(["get", "/envs/{name}", init, "foo"])
  })

  it("fills index 2 with null — never the cluster — for parameterless operations", () => {
    const key = clusterQueryKey("get", "/envs", undefined, "foo")
    expect(key[2]).toBeNull()
    expect(key[3]).toBe("foo")
    // Spreading the filler must be a no-op: the library does `{signal, ...init}`,
    // and a string there would explode into {0: "a", 1: "r", …}.
    expect({ ...(key[2] as object) }).toEqual({})
  })

  it("leaves existing prefix-based invalidations matching", () => {
    // lib/queries invalidates with ["get", "/envs"] and
    // ["get", "/envs/{name}/sandboxpools", {params}] — both must stay prefixes.
    const listKey = clusterQueryKey("get", "/envs", undefined, "foo")
    expect(listKey.slice(0, 2)).toEqual(["get", "/envs"])

    const init = { params: { path: { name: "slimedev" } } }
    const poolsKey = clusterQueryKey("get", "/envs/{name}/sandboxpools", init, "foo")
    expect(poolsKey.slice(0, 3)).toEqual(["get", "/envs/{name}/sandboxpools", init])
  })

  it("does not let a list key collide with a sibling resource path", () => {
    expect(clusterQueryKey("get", "/envs", undefined, "foo")[1]).not.toBe("/envs/{name}")
  })
})

describe("getApiClient", () => {
  it("scopes the keys it hands out, so two clusters cannot share a cache entry", async () => {
    const { getApiClient } = await import("@/lib/api/client")

    const foo = getApiClient("foo").queryOptions("get", "/envs/{name}", {
      params: { path: { name: "slimedev" } },
    })
    const bar = getApiClient("bar").queryOptions("get", "/envs/{name}", {
      params: { path: { name: "slimedev" } },
    })

    // Indexed through `unknown[]`: openapi-react-query types queryKey as a
    // 3-tuple, so the appended cluster is invisible to the compiler even though
    // it is there at runtime. App code only ever passes the key wholesale.
    expect(foo.queryKey).not.toEqual(bar.queryKey)
    expect((foo.queryKey as readonly unknown[])[3]).toBe("foo")
    expect((bar.queryKey as readonly unknown[])[3]).toBe("bar")
    // The library's queryFn must survive the override.
    expect(typeof foo.queryFn).toBe("function")
  })

  it("fills index 2 for operations without init", async () => {
    const { getApiClient } = await import("@/lib/api/client")
    const opts = getApiClient("foo").queryOptions("get", "/envs")
    expect(opts.queryKey).toEqual(["get", "/envs", null, "foo"])
  })
})
