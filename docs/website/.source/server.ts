// @ts-nocheck
import * as __fd_glob_5 from "../content/docs/tutorials/mini-swe-agent.mdx?collection=docs"
import * as __fd_glob_4 from "../content/docs/tutorials/e2b.mdx?collection=docs"
import * as __fd_glob_3 from "../content/docs/index.mdx?collection=docs"
import { default as __fd_glob_2 } from "../content/docs/tutorials/meta.json?collection=docs"
import { default as __fd_glob_1 } from "../content/docs/api/meta.json?collection=docs"
import { default as __fd_glob_0 } from "../content/docs/meta.json?collection=docs"
import { server } from 'fumadocs-mdx/runtime/server';
import type * as Config from '../source.config';

const create = server<typeof Config, import("fumadocs-mdx/runtime/types").InternalTypeConfig & {
  DocData: {
  }
}>({"doc":{"passthroughs":["extractedReferences"]}});

export const docs = await create.docs("docs", "content/docs", {"meta.json": __fd_glob_0, "api/meta.json": __fd_glob_1, "tutorials/meta.json": __fd_glob_2, }, {"index.mdx": __fd_glob_3, "tutorials/e2b.mdx": __fd_glob_4, "tutorials/mini-swe-agent.mdx": __fd_glob_5, });