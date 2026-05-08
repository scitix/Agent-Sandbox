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

import { SVGProps } from "react"
const AgentBoxIcon = (props: SVGProps<SVGSVGElement>) => (
  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1080 1080" {...props}>
    <defs>
      <linearGradient
        id="a"
        x1={1.216}
        x2={0.392}
        y1={0.165}
        y2={0.653}
        data-name="sandbox 22"
      >
        <stop offset={0} stopColor="#ffb05a" />
        <stop offset={1} stopColor="#d8355b" />
      </linearGradient>
      <linearGradient
        id="b"
        x1={0.782}
        x2={0.277}
        y1={0.18}
        y2={0.501}
        data-name="sandbox 13"
      >
        <stop offset={0} stopColor="#ffb15c" />
        <stop offset={1} stopColor="#ff735c" />
      </linearGradient>
      <linearGradient
        id="c"
        x1={0.282}
        x2={0.882}
        y1={0.913}
        y2={0.131}
        data-name="sandbox 24"
      >
        <stop offset={0} stopColor="#ff994d" />
        <stop offset={1} stopColor="#d8355b" />
      </linearGradient>
      <linearGradient
        id="d"
        x1={0.017}
        x2={0.947}
        y1={1.043}
        y2={0.148}
        data-name="sandbox 25"
      >
        <stop offset={0} stopColor="#ff994d" />
        <stop offset={1} stopColor="#b82446" />
      </linearGradient>
    </defs>
    <title>{"Layer 1"}</title>
    <path
      fill="url(#a)"
      d="m1038.893 340.97 1.4 541.65-201.25 110.21-202.98-424.81 402.83-227.05z"
      className="cls-3"
    />
    <path
      fill="url(#b)"
      d="m569.093 66.83 469.8 274.14-402.83 227.05-209.62-432.83 142.65-68.36z"
      className="cls-2"
    />
    <path
      fill="url(#c)"
      d="m522.863 334.28-113.29 256.89c-91.2 51.82-170.65 99.83-242.86 176.96l-26.48 28.25 286.22-661.19 96.41 199.09z"
      className="cls-1"
    />
    <path
      fill="url(#d)"
      d="m711.723 725.93-172.5 80.77c-97.14 45.49-183.56 108.25-256.87 186.54l-8.44 9.01H34.173l120.43-128.62c88.59-94.61 193.03-170.45 310.41-225.41l171.28-80.2 75.43 157.91z"
      className="cls-4"
    />
  </svg>
)
export default AgentBoxIcon
