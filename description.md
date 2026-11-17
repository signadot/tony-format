# docs: an Articles group in the site nav, holding the four published articles and the logd store article

The site has no place for long-form articles. Four are published on dev.to and live nowhere in the repository; two are drafts under `docs/articles/`, excluded from the build.

- Add the four published articles to `docs/articles/`, each with front matter naming where it was published (`canonical_url`, `date`): One Source, Two Outputs; Structured Matching, Patching, and Diffing Done Right; Evaluation in Tony Format; How to Navigate ArgoCD's Manifest Friction with ORAS and `o build`.
- Add a top-level `Articles` group to the nav with those four and `logd-document-store.md`, which stops being a draft.
- `logd-streaming-apply.md` stays a draft and stays excluded.
- An article's title is in its front matter, for its destination, and its body has no H1. The site has to render one without the body gaining a heading its destination would show twice.