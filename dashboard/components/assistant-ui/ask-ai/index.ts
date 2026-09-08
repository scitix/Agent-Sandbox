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

// The Ask AI button family: many designs / prompts, one shared behavior
// (`useAskAI`). Each variant owns its own prompt (and attachment, when content
// is involved), collected here so they're easy to find and keep consistent.
export { AskAIButton, type AskAIButtonProps } from './ask-ai-button'
export { useAskAI, type AskInput } from './use-ask-ai'
export { TableAskAIButton } from './table-ask-ai-button'
