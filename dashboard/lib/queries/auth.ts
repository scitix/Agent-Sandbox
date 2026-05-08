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

// Auth mutations (login/mock-login — no cache invalidation needed)

import { useMutation } from "@tanstack/react-query"
import { login, iamLogin } from "@/lib/api/client"
import type { LoginInput, IamLoginInput } from "@/lib/api/client"

export function useLogin() {
  return useMutation({
    mutationFn: (input: LoginInput) => login(input),
  })
}

export function useMockLogin() {
  return useMutation({
    mutationFn: (input: IamLoginInput) => iamLogin(input),
  })
}

/** @deprecated Use useMockLogin instead */
export const useIamLogin = useMockLogin
