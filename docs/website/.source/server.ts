// @ts-nocheck
import * as __fd_glob_39 from "../content/docs/examples/templates/index.mdx?collection=docs"
import * as __fd_glob_38 from "../content/docs/examples/templates/e2b.mdx?collection=docs"
import * as __fd_glob_37 from "../content/docs/examples/templates/e2b-kata.mdx?collection=docs"
import * as __fd_glob_36 from "../content/docs/examples/templates/e2b-docker.mdx?collection=docs"
import * as __fd_glob_35 from "../content/docs/examples/envs/index.mdx?collection=docs"
import * as __fd_glob_34 from "../content/docs/examples/envs/e2b.mdx?collection=docs"
import * as __fd_glob_33 from "../content/docs/examples/envs/e2b-kata.mdx?collection=docs"
import * as __fd_glob_32 from "../content/docs/examples/envs/e2b-docker.mdx?collection=docs"
import * as __fd_glob_31 from "../content/docs/tutorials/mini-swe-agent.mdx?collection=docs"
import * as __fd_glob_30 from "../content/docs/tutorials/harbor-benchmarks.mdx?collection=docs"
import * as __fd_glob_29 from "../content/docs/tutorials/e2b.mdx?collection=docs"
import * as __fd_glob_28 from "../content/docs/tutorials/cli.mdx?collection=docs"
import * as __fd_glob_27 from "../content/docs/skills/index.mdx?collection=docs"
import * as __fd_glob_26 from "../content/docs/skills/abx-sandbox-secrets.mdx?collection=docs"
import * as __fd_glob_25 from "../content/docs/skills/abx-sandbox-network.mdx?collection=docs"
import * as __fd_glob_24 from "../content/docs/skills/abx-sandbox-docker.mdx?collection=docs"
import * as __fd_glob_23 from "../content/docs/skills/abx-resource-capacity.mdx?collection=docs"
import * as __fd_glob_22 from "../content/docs/skills/abx-reinforcement-learning.mdx?collection=docs"
import * as __fd_glob_21 from "../content/docs/skills/abx-observe.mdx?collection=docs"
import * as __fd_glob_20 from "../content/docs/skills/abx-managed-agent.mdx?collection=docs"
import * as __fd_glob_19 from "../content/docs/skills/abx-harbor-framework.mdx?collection=docs"
import * as __fd_glob_18 from "../content/docs/skills/abx-common.mdx?collection=docs"
import * as __fd_glob_17 from "../content/docs/concepts/templates.mdx?collection=docs"
import * as __fd_glob_16 from "../content/docs/concepts/pools.mdx?collection=docs"
import * as __fd_glob_15 from "../content/docs/concepts/inplace-update.mdx?collection=docs"
import * as __fd_glob_14 from "../content/docs/concepts/index.mdx?collection=docs"
import * as __fd_glob_13 from "../content/docs/concepts/envs.mdx?collection=docs"
import * as __fd_glob_12 from "../content/docs/concepts/egress-and-secrets.mdx?collection=docs"
import * as __fd_glob_11 from "../content/docs/concepts/cross-cluster.mdx?collection=docs"
import * as __fd_glob_10 from "../content/docs/concepts/autoscaling.mdx?collection=docs"
import * as __fd_glob_9 from "../content/docs/installation.mdx?collection=docs"
import * as __fd_glob_8 from "../content/docs/index.mdx?collection=docs"
import { default as __fd_glob_7 } from "../content/docs/examples/templates/meta.json?collection=docs"
import { default as __fd_glob_6 } from "../content/docs/examples/envs/meta.json?collection=docs"
import { default as __fd_glob_5 } from "../content/docs/tutorials/meta.json?collection=docs"
import { default as __fd_glob_4 } from "../content/docs/skills/meta.json?collection=docs"
import { default as __fd_glob_3 } from "../content/docs/examples/meta.json?collection=docs"
import { default as __fd_glob_2 } from "../content/docs/concepts/meta.json?collection=docs"
import { default as __fd_glob_1 } from "../content/docs/api/meta.json?collection=docs"
import { default as __fd_glob_0 } from "../content/docs/meta.json?collection=docs"
import { server } from 'fumadocs-mdx/runtime/server';
import type * as Config from '../source.config';

const create = server<typeof Config, import("fumadocs-mdx/runtime/types").InternalTypeConfig & {
  DocData: {
  }
}>({"doc":{"passthroughs":["extractedReferences"]}});

export const docs = await create.docs("docs", "content/docs", {"meta.json": __fd_glob_0, "api/meta.json": __fd_glob_1, "concepts/meta.json": __fd_glob_2, "examples/meta.json": __fd_glob_3, "skills/meta.json": __fd_glob_4, "tutorials/meta.json": __fd_glob_5, "examples/envs/meta.json": __fd_glob_6, "examples/templates/meta.json": __fd_glob_7, }, {"index.mdx": __fd_glob_8, "installation.mdx": __fd_glob_9, "concepts/autoscaling.mdx": __fd_glob_10, "concepts/cross-cluster.mdx": __fd_glob_11, "concepts/egress-and-secrets.mdx": __fd_glob_12, "concepts/envs.mdx": __fd_glob_13, "concepts/index.mdx": __fd_glob_14, "concepts/inplace-update.mdx": __fd_glob_15, "concepts/pools.mdx": __fd_glob_16, "concepts/templates.mdx": __fd_glob_17, "skills/abx-common.mdx": __fd_glob_18, "skills/abx-harbor-framework.mdx": __fd_glob_19, "skills/abx-managed-agent.mdx": __fd_glob_20, "skills/abx-observe.mdx": __fd_glob_21, "skills/abx-reinforcement-learning.mdx": __fd_glob_22, "skills/abx-resource-capacity.mdx": __fd_glob_23, "skills/abx-sandbox-docker.mdx": __fd_glob_24, "skills/abx-sandbox-network.mdx": __fd_glob_25, "skills/abx-sandbox-secrets.mdx": __fd_glob_26, "skills/index.mdx": __fd_glob_27, "tutorials/cli.mdx": __fd_glob_28, "tutorials/e2b.mdx": __fd_glob_29, "tutorials/harbor-benchmarks.mdx": __fd_glob_30, "tutorials/mini-swe-agent.mdx": __fd_glob_31, "examples/envs/e2b-docker.mdx": __fd_glob_32, "examples/envs/e2b-kata.mdx": __fd_glob_33, "examples/envs/e2b.mdx": __fd_glob_34, "examples/envs/index.mdx": __fd_glob_35, "examples/templates/e2b-docker.mdx": __fd_glob_36, "examples/templates/e2b-kata.mdx": __fd_glob_37, "examples/templates/e2b.mdx": __fd_glob_38, "examples/templates/index.mdx": __fd_glob_39, });