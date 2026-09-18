// @ts-nocheck
import * as __fd_glob_27 from "../content/docs/concepts/skills/index.mdx?collection=docs"
import * as __fd_glob_26 from "../content/docs/concepts/skills/abx-sandbox-secrets.mdx?collection=docs"
import * as __fd_glob_25 from "../content/docs/concepts/skills/abx-sandbox-network.mdx?collection=docs"
import * as __fd_glob_24 from "../content/docs/concepts/skills/abx-sandbox-docker.mdx?collection=docs"
import * as __fd_glob_23 from "../content/docs/concepts/skills/abx-resource-capacity.mdx?collection=docs"
import * as __fd_glob_22 from "../content/docs/concepts/skills/abx-reinforcement-learning.mdx?collection=docs"
import * as __fd_glob_21 from "../content/docs/concepts/skills/abx-observe.mdx?collection=docs"
import * as __fd_glob_20 from "../content/docs/concepts/skills/abx-managed-agent.mdx?collection=docs"
import * as __fd_glob_19 from "../content/docs/concepts/skills/abx-harbor-framework.mdx?collection=docs"
import * as __fd_glob_18 from "../content/docs/concepts/skills/abx-common.mdx?collection=docs"
import * as __fd_glob_17 from "../content/docs/tutorials/mini-swe-agent.mdx?collection=docs"
import * as __fd_glob_16 from "../content/docs/tutorials/e2b.mdx?collection=docs"
import * as __fd_glob_15 from "../content/docs/tutorials/cli.mdx?collection=docs"
import * as __fd_glob_14 from "../content/docs/concepts/templates.mdx?collection=docs"
import * as __fd_glob_13 from "../content/docs/concepts/pools.mdx?collection=docs"
import * as __fd_glob_12 from "../content/docs/concepts/inplace-update.mdx?collection=docs"
import * as __fd_glob_11 from "../content/docs/concepts/index.mdx?collection=docs"
import * as __fd_glob_10 from "../content/docs/concepts/envs.mdx?collection=docs"
import * as __fd_glob_9 from "../content/docs/concepts/egress-and-secrets.mdx?collection=docs"
import * as __fd_glob_8 from "../content/docs/concepts/cross-cluster.mdx?collection=docs"
import * as __fd_glob_7 from "../content/docs/concepts/autoscaling.mdx?collection=docs"
import * as __fd_glob_6 from "../content/docs/installation.mdx?collection=docs"
import * as __fd_glob_5 from "../content/docs/index.mdx?collection=docs"
import { default as __fd_glob_4 } from "../content/docs/concepts/skills/meta.json?collection=docs"
import { default as __fd_glob_3 } from "../content/docs/tutorials/meta.json?collection=docs"
import { default as __fd_glob_2 } from "../content/docs/concepts/meta.json?collection=docs"
import { default as __fd_glob_1 } from "../content/docs/api/meta.json?collection=docs"
import { default as __fd_glob_0 } from "../content/docs/meta.json?collection=docs"
import { server } from 'fumadocs-mdx/runtime/server';
import type * as Config from '../source.config';

const create = server<typeof Config, import("fumadocs-mdx/runtime/types").InternalTypeConfig & {
  DocData: {
  }
}>({"doc":{"passthroughs":["extractedReferences"]}});

export const docs = await create.docs("docs", "content/docs", {"meta.json": __fd_glob_0, "api/meta.json": __fd_glob_1, "concepts/meta.json": __fd_glob_2, "tutorials/meta.json": __fd_glob_3, "concepts/skills/meta.json": __fd_glob_4, }, {"index.mdx": __fd_glob_5, "installation.mdx": __fd_glob_6, "concepts/autoscaling.mdx": __fd_glob_7, "concepts/cross-cluster.mdx": __fd_glob_8, "concepts/egress-and-secrets.mdx": __fd_glob_9, "concepts/envs.mdx": __fd_glob_10, "concepts/index.mdx": __fd_glob_11, "concepts/inplace-update.mdx": __fd_glob_12, "concepts/pools.mdx": __fd_glob_13, "concepts/templates.mdx": __fd_glob_14, "tutorials/cli.mdx": __fd_glob_15, "tutorials/e2b.mdx": __fd_glob_16, "tutorials/mini-swe-agent.mdx": __fd_glob_17, "concepts/skills/abx-common.mdx": __fd_glob_18, "concepts/skills/abx-harbor-framework.mdx": __fd_glob_19, "concepts/skills/abx-managed-agent.mdx": __fd_glob_20, "concepts/skills/abx-observe.mdx": __fd_glob_21, "concepts/skills/abx-reinforcement-learning.mdx": __fd_glob_22, "concepts/skills/abx-resource-capacity.mdx": __fd_glob_23, "concepts/skills/abx-sandbox-docker.mdx": __fd_glob_24, "concepts/skills/abx-sandbox-network.mdx": __fd_glob_25, "concepts/skills/abx-sandbox-secrets.mdx": __fd_glob_26, "concepts/skills/index.mdx": __fd_glob_27, });