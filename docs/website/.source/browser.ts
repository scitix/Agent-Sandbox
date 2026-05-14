// @ts-nocheck
import { browser } from 'fumadocs-mdx/runtime/browser';
import type * as Config from '../source.config';

const create = browser<typeof Config, import("fumadocs-mdx/runtime/types").InternalTypeConfig & {
  DocData: {
  }
}>();
const browserCollections = {
  docs: create.doc("docs", {"index.mdx": () => import("../content/docs/index.mdx?collection=docs"), "tutorials/e2b.mdx": () => import("../content/docs/tutorials/e2b.mdx?collection=docs"), "tutorials/mini-swe-agent.mdx": () => import("../content/docs/tutorials/mini-swe-agent.mdx?collection=docs"), }),
};
export default browserCollections;