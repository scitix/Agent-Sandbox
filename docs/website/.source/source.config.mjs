// source.config.ts
import { defineDocs, defineConfig } from "fumadocs-mdx/config";
import { remarkMdxMermaid } from "fumadocs-core/mdx-plugins";
var docs = defineDocs({
  dir: "content/docs",
  docs: {
    postprocess: {
      includeProcessedMarkdown: true
    }
  }
});
var source_config_default = defineConfig({
  mdxOptions: {
    // ```mermaid blocks become <Mermaid chart="…" />, which components/mermaid.tsx
    // draws in the browser. The fence stays a fence in the source, so the `.md`
    // twin an agent fetches carries the diagram as text it can read.
    remarkPlugins: [remarkMdxMermaid]
  }
});
export {
  source_config_default as default,
  docs
};
