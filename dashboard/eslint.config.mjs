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

import { defineConfig, globalIgnores } from "eslint/config"
import nextVitals from "eslint-config-next/core-web-vitals"
import nextTs from "eslint-config-next/typescript"
import prettier from "eslint-config-prettier/flat"

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  prettier,
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "components/ui/**",
    "build/**",
    "next-env.d.ts",
    // shadcn-generated, lives outside components/ui (installed by the sidebar
    // block, overwritten on `shadcn add`) — don't lint upstream code.
    "hooks/use-mobile.ts",
  ]),
  {
    // The assistant's transport and runtime layers, ported from a working
    // implementation that lints against eslint-plugin-react-hooks v5. We get
    // v6 transitively through eslint-config-next, and three of its rules —
    // added for React Compiler analysability — flag idioms this code uses on
    // purpose:
    //
    //   * a ref written during render, because the value is read from a STORE
    //     SUBSCRIPTION that fires before an effect could update a mirror — and
    //     the transition into "waiting on the user" is exactly that moment. An
    //     effect-written mirror makes the rejoin logic read a stale value for
    //     one frame, which shows a turn's reply twice or not at all.
    //   * setState in an effect, where the effect is reconciling with the
    //     agent runtime's own store rather than deriving from props.
    //   * mutation of the AG-UI agent object, which is deliberately NOT React
    //     state: rebuilding it per render resets its abort controller mid-run.
    //
    // Scoped to these three files rather than the directory, so anything new
    // written here still gets the rules. Revisit if the runtime is rewritten
    // against the compiler rather than ported.
    files: [
      "components/assistant-ui/gw/use-gateway-runtime.tsx",
      "components/assistant-ui/gw/gateway-host.tsx",
      "components/assistant-ui/runtime-provider.tsx",
    ],
    rules: {
      "react-hooks/refs": "off",
      "react-hooks/set-state-in-effect": "off",
      "react-hooks/immutability": "off",
    },
  },
  {
    rules: {
      // useReactTable returns non-memoizable functions — known library limitation
      "react-hooks/incompatible-library": "off",
      // actionTypes is used as a type via `typeof actionTypes`
      "@typescript-eslint/no-unused-vars": [
        "error",
        { varsIgnorePattern: "^_", argsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_" },
      ],
    },
  },
])

export default eslintConfig
