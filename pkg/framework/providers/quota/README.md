
`TemplateLabelPolicy` is an optional pure provider interface. `LabelsFor` returns
edition-owned classification labels (or nil for providers without this policy).
The host stamps them on Envs and Pod templates before hashing revisions; this
keeps edition rules out of the shared renderer and requires no lifecycle hook.
